package main

/**** this is the main entrypoint of mauth service

mauth is a webservice used for authorization, authentication, user registration
it uses either a file or an sql as permanent storage
it will use a local cache on a per user basis to store authorization

mauth regroups users, actions, roles (described below) in Domain.
users are unique among a specific domain but may be duplicated in different domains.
Same goes for actions and roles.

** Authorization model

users are represented as User struct.
Actions represent atomic taks whose access has to be verified.
Actions are gathered into Roles, to simplify authorization management. One action may belong to multiple roles
Actions are applied to Objects. To check if a user may  trigger an action on an object,
we have to check if the user has the right to. That's what Rights are for :
a Right is an assignment of a Role to a specific User for a specific object
Through Rights, any User may be assigned multiple Roles, applying to one Object at least.
Rights may have an expiration date for temporary access grants

Objects are not managed by mauth. When a right is granted to a user, an external object ID has to be provided
mauth will then store the right as the following struct : (user, role, object_id, optional expiration date)

The object_id is a string that describes the object. The string format is up to the developer to choose but we advise
to use the following structure :
object_type(object_id)
This allows for tree-based organization of object.
For example, let's say a car belongs to a fleet of cars in a garage which belongs to a company
A user, let's call him Bob,  may have the right to drive this car (role = driver), open any car in the garage (role = maintainer),
and list any of the cars in company ( role = inventory )
So the Bob's rights would be :
Bob, driver, car(123)
Bob, maintainer, garage(28)
Bob, inventory, company(9)

To check Bob's right to trigger an action on a specific car, a call will be maid to mauth service with the following
parameters : ID of bob, tree of car ID : company(9)garage(28)car(123), task name (carOpen)

** Authentication model

mauth uses :
- the jwt mechanism to create auth token
- API key for specific key access

To authenticate an user, an app has to POST {mauth_url}/user/auth
Request should contain contain :
- a domain token as header values
- user credential (login & pwd) as post values

In return, mauth sends a JWT for the session along with a JWT for renewal






*/

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/marcmorel/morm"
	"github.com/rs/cors"
	"gitlab.com/hiveway/getaround-api-catch/cmd/apicall"
	"gitlab.com/hiveway/getaround-api-catch/cmd/dispatcher"
	"gitlab.com/hiveway/getaround-api-catch/cmd/getaround"
	"gitlab.com/hiveway/getaround-api-catch/cmd/job"
	"gitlab.com/hiveway/getaround-api-catch/cmd/progress"
	"gitlab.com/hiveway/getaround-api-catch/cmd/slack"
	"gitlab.com/hiveway/getaround-api-catch/cmd/tools"
)

var dataSource string
var getaroundWrapper *apicall.Wrapper
var environment string // either dev / staging / prod
var serverMode = ""
var runningMode = ""
var globalConfig map[string](map[string]string) // will hold major config elements

func downloadConfig(bucket string, path string) (map[string]string, error) {
	conf, err := tools.GetContentFromS3(bucket, path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	if err := json.Unmarshal(conf, &result); err != nil {
		return nil, errors.New(err.Error() + " in " + string(conf))
	}
	return result, err
}
func setupConfig() {
	metadata := os.Getenv("ECS_CONTAINER_METADATA_URI")
	fmt.Printf("Metadata found : %s\n", metadata)

	dataSource = os.Getenv("DATASOURCE")
	if len(dataSource) == 0 {
		fmt.Printf("No data source found for container")
		os.Exit(1)
	}
	runningMode = os.Getenv("MODE")
	environment = strings.ToLower(os.Getenv("ENVIRONMENT"))
	if environment == "" {
		environment = "dev"
	}
	if environment != "dev" && environment != "staging" && environment != "prod" {
		fmt.Printf(environment + " is an unknown environment")
		panic(environment + " is an unknown environment")
	}

	globalConfig = make(map[string]map[string]string)

	//downloadconfig for google spreadsheet addresses
	proxyconfig, err := downloadConfig("upload.fleet.live.s3", "proxyconfig/"+environment+".json")
	if err != nil {
		fmt.Printf(err.Error())
		panic(err.Error())
	}
	globalConfig["proxy"] = proxyconfig
}
func main() {

	morm.InitModels(
		[]interface{}{
			&getaround.Account{},
			&getaround.Alert{},
			&getaround.Car{},
			&getaround.Calendar{},
			&getaround.Picture{},
			&getaround.LocationHistory{},
			&job.Job{},
		})

	//init event dispatcher
	dispatcher.CreateChannel("getaround")
	dispatcher.Channels["getaround"].Subscribe("", eventlog)
	setupConfig()
	fmt.Printf("Starting in " + environment + " environment as a " + runningMode + "\n")

	slackConf, _ := tools.GetContentFromS3("upload.fleet.live.s3", "slackconfig/"+environment+".json")
	slackMap := map[string]string{}
	if err := json.Unmarshal(slackConf, &slackMap); err != nil {
		panic("Unable to decode slack conf file : " + err.Error())
	}

	slack.InitChannels(slackMap)
	delayAlert := 180
	delayCalendar := 400

	getaround.MigrateDB(dataSource)
	if _, err := morm.InitDB(dataSource); err != nil {
		fmt.Printf("Error in DB connection :%s\n", err.Error())
		os.Exit(1)
	}

	getaroundWrapper, _ = apicall.CreateWrapper(globalConfig["proxy"])

	var wg sync.WaitGroup
	if runningMode == "worker" {
		wg.Add(3)
		slack.PublishTo("igor",
			"Igor a redémarré en mode worker et en environnement "+environment+" à "+
				time.Now().Format("15:04:05")+" UTC. Les alertes sont surveillées toutes les "+
				strconv.Itoa(delayAlert)+" secondes. Les locations sont surveillées toutes les "+
				strconv.Itoa(delayCalendar)+" secondes."+
				" Bonne journée :-)", "text")

		go func() {
			fmt.Printf("Starting update alert thread\n")
			updateAlerts(getaroundWrapper, delayAlert)
			wg.Done()
		}()
		go func() {
			fmt.Printf("Starting update events thread\n")

			updateEvents(getaroundWrapper, delayCalendar)
			wg.Done()
		}()
		go func() {
			fmt.Printf("Starting health check server\n")
			initHealthOnlyServer()
			wg.Done()
		}()
	}
	if runningMode == "server" {
		wg.Add(1)
		go func() {
			fmt.Printf("Starting HTTP server thread\n")
			slack.PublishTo("igor", "Igor a redémarré en mode server et en environnement "+environment+" à "+
				time.Now().Format("15:04:05")+" UTC. Bonne journée :-)", "text")
			initServer(getaroundWrapper)
			wg.Done()
		}()
	}

	wg.Wait()

}

func initHealthOnlyServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	router := chi.NewRouter()
	router.Get("/check", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	log.Fatal(http.ListenAndServe(":"+port, router))

}
func lastRentals(w http.ResponseWriter, r *http.Request) {
	arrmaps, err := morm.FindAllByColumn("calendar", map[string]string{"type": `"rentals"`, "morm_limit": "150", "morm_orderby": "ends_at desc", "morm_verbose": ""})

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(`{"status":"Error while fetching calendars : ` + err.Error() + `"}`))
	}
	if arrmaps == nil {
		w.WriteHeader(500)
		w.Write([]byte(`{"status":"No rentals"}`))
	}

	result := tools.ExtractEncodedArrayFields(map[string]string{
		"car_getaround_id":   "calendar.car_getaround_id",
		"geoloc":             "calendar.geoloc",
		"starts_at":          "calendar.starts_at",
		"ends_at":            "calendar.ends_at",
		"phone_number":       "calendar.phone_number",
		"getaround_id":       "calendar.getaround_id",
		"id":                 "calendar.id",
		"state":              "calendar.state",
		"rated_at":           "calendar.rated_at",
		"rental_end_map":     "calendar.rental_end_map",
		"driver_comment":     "calendar.driver_comment",
		"rental_ending_time": "calendar.rental_ending_time",
	}, arrmaps)

	for _, m := range result {
		car, _ := getaround.GetCarByGetaroundID(m["car_getaround_id"])
		if car != nil {
			m["car_registration"] = car.PlateNumber
			m["car_name"] = car.Title
		}
	}
	js, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(`{"status":"Error while converting calendars : ` + err.Error() + `"}`))
	}
	w.Write([]byte(`{ "status":"ok","rentals":` + string(js) + `}`))

}
func rentalDetail(w http.ResponseWriter, r *http.Request) {
	rentalID := chi.URLParam(r, "rentalID") // from a route like /users/{userID}

	cal, err := getaround.GetCalendarByID(rentalID)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(`{"status":"Error while fetching calendars : ` + err.Error() + `"}`))
	}
	if cal == nil {
		w.WriteHeader(404)
		w.Write([]byte(`{"status":"Calendar not found"}`))
	}
	cal.WithPics() //enrich the cal with pics
	cal.WithCar()
	cal.WithWithPreviousRentals(200)

	/*result := map[string]interface{}{
		"car_getaround_id":   cal.CarGetaroundID,
		"geoloc":             cal.Geoloc,
		"starts_at":          cal.StartsAt,
		"ends_at":            cal.EndsAt,
		"phone_number":       cal.PhoneNumber,
		"getaround_id":       cal.GetaroundID,
		"id":                 cal.ID,
		"state":              cal.State,
		"rated_at":           cal.RatedAt,
		"rental_end_map":     cal.RentalEndMap,
		"driver_comment":     cal.DriverComment,
		"rental_ending_time": cal.RentalEndingTime,
		"car_registration":   cal.Car.PlateNumber,
		"car_name":           cal.Car.Title,
	}
	pics := make([]map[string]string, len(cal.Pictures))
	for i, p := range cal.Pictures {
		pics[i] = make(map[string]string)
		pics[i]["url"] = p.URL
		pics[i]["location"] = p.Location
		pics[i]["type"] = p.Type
	}
	result["pictures"] = pics*/
	js, err := json.Marshal(cal)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(`{"status":"Error while converting calendar : ` + err.Error() + `"}`))
	}
	w.Write([]byte(`{ "status":"ok","rental":` + string(js) + `}`))

}

func sendHTTP(w http.ResponseWriter, code int, payload interface{}) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")

	switch v := payload.(type) {
	case string:
		w.Write([]byte(v))
	case []byte:
		w.Write(v)
	case map[string]interface{}:
		buff, _ := json.Marshal(v)
		w.Write([]byte(buff))
	default:
		buff, _ := json.Marshal(v)
		w.Write([]byte(buff))
	}
}
func initServer(wrapper *apicall.Wrapper) {

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	router := chi.NewRouter()
	// Add CORS middleware around every request
	// See https://github.com/rs/cors for full option listing
	router.Use(cors.New(cors.Options{
		AllowedOrigins:     []string{"*"},
		Debug:              false,
		AllowedMethods:     []string{"GET", "POST", "OPTIONS"},
		OptionsPassthrough: false,
		AllowCredentials:   true}).Handler)

	router.Get("/check", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Printf("Check appelé\n")

		w.Write([]byte("ok"))

	})

	router.Post("/slack", func(w http.ResponseWriter, r *http.Request) {
		slack.Command(r.PostFormValue("command"), r.PostFormValue("text"), w, wrapper)
	})
	router.Get("/price/refreshprice", func(w http.ResponseWriter, r *http.Request) {
		tracker, err := job.CreateJobTracker("refreshprice", progress.JobStarted)
		if err != nil {
			sendHTTP(w, 500, map[string]interface{}{"status": "error", "error": "unable to create tracker :" + err.Error()})
			return
		}
		go getaround.PublishPricesToSpreadsheet(wrapper, *tracker)

		sendHTTP(w, 200, map[string]interface{}{"status": "ok", "id": tracker.Payload.(*job.Job).ID})
	})
	router.Get("/price/updateprice", func(w http.ResponseWriter, r *http.Request) {
		tracker, err := job.CreateJobTracker("updateprice", progress.JobStarted)
		if err != nil {
			sendHTTP(w, 500, map[string]interface{}{"status": "error", "error": "unable to create tracker :" + err.Error()})
			return
		}
		values := r.URL.Query()
		keys, ok := values["fake"]
		fakePriceUpdate := true
		if ok && len(keys[0]) > 0 && keys[0] == "false" {
			fakePriceUpdate = false
		}
		go getaround.UpdatePricesFromSpreadsheet(wrapper, *tracker, fakePriceUpdate)

		sendHTTP(w, 200, map[string]interface{}{"status": "ok", "id": tracker.Payload.(*job.Job).ID})
	})
	router.Get("/getlocation", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			car, err := getaround.GetCarByGetaroundID("261423")
			if car == nil {
				fmt.Printf("Voiture introuvable\n")
				return
			}
			if err != nil {
				fmt.Printf("erreur saisie\n")
				return
			}
			loc, err := car.GetLocation(wrapper)
			if err != nil {
				fmt.Printf("erreur localisation:%s\n", err.Error())
				return
			}
			fmt.Printf("Localisation : %s\n", loc)
		}()
	})
	router.Get("/job/status/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id") // from a route like /users/{userID}
		_, err := strconv.Atoi(id)
		if id == "" || err != nil {
			sendHTTP(w, 500, map[string]interface{}{"status": "error", "error": "ID must be an integer"})
			return
		}
		j, err := job.GetJobByID(id)
		if err != nil {
			sendHTTP(w, 404, map[string]interface{}{"status": "error", "error": "Job not found"})
			return
		}
		sendHTTP(w, 200, map[string]interface{}{"status": "ok", "jobstatus": j.Status, "progress": j.Progress, "log": j.Log})
	})

	router.Route("/rentals", func(router chi.Router) {
		router.Get("/last", lastRentals)
	})

	router.Get("/rental/{rentalID}", rentalDetail)

	router.Get("/unavailable_cars", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		end := start.AddDate(0, 0, 7)
		result, err := getaround.GetUnavailableCars(&start, &end)
		if err != nil {
			w.Write([]byte("<html><body>Error : " + err.Error() + "</body></html>"))
			return
		}
		html := "<html><body><h2>Véhicules indisponibles :</h2>\n"
		html += "<table>\n"
		for _, m := range result {
			template := "<tr><td>{{account_name}}</td><td>{{plate_number}}</td>"
			template += "<td>{{title}}</td><td>{{time}}</td></tr>\n"
			dates := ""
			startsAt := morm.SafeTime(m["starts_at"])
			if startsAt != nil {
				dates += "du " + startsAt.Format("02/01/2006")
			}
			endsAt := morm.SafeTime(m["ends_at"])
			if endsAt != nil {
				dates += " au " + endsAt.Format("02/01/2006")
			}
			m["time"] = dates
			for k, v := range m {
				var str sql.NullString
				str.Scan(v)
				template = strings.ReplaceAll(template, "{{"+k+"}}", str.String)
			}

			html += template
		}
		html += "</table>\n"

		cars, err := getaround.GetInactiveCars()
		if err != nil {
			html += err.Error()
		}
		if cars != nil {
			html += "<h2>Véhicules désactivés :</h2>\n"
			html += "<table>\n"
			for _, c := range cars {
				acc, _ := getaround.GetAccountByID(strconv.FormatUint(c.AccountID, 10))
				html += "<tr><td>" + acc.AccountName + "</td><td>" + c.PlateNumber + "</td><td>" + c.Title + "</td></tr>\n"
			}
			html += "</table>\n"
		}
		html += "</body></html>"
		w.Write([]byte(html))
	})

	log.Fatal(http.ListenAndServe(":"+port, router))

}

func updateAlerts(w *apicall.Wrapper, delaySecond int) {
	fmt.Printf("Update alerts started\n")

	for {

		accounts, err := getaround.GetAccounts()
		if err != nil {
			fmt.Printf("Error on getaccounts %s\n", err.Error())
			return
		}

		for _, acc := range accounts {
			err := acc.GetAlerts(w)
			if err != nil {
				fmt.Printf("Error on getalerts for account %s : %s\n", acc.AccountName, err.Error())
			}
		}
		time.Sleep(time.Duration(delaySecond) * time.Second)
	}

}
func updateEvents(w *apicall.Wrapper, delaySecond int) {
	fmt.Printf("Update rentals started\n")
	for {

		start := time.Now().AddDate(0, 0, -10)

		for week := 0; week < 4; week++ {
			start := start.AddDate(0, 0, week*7)
			end := start.AddDate(0, 0, 7)
			accounts, err := getaround.GetAccounts()
			if err != nil {
				fmt.Printf("Error on getaccounts %s\n", err.Error())
				return
			}
			fmt.Printf("Getting rentals for the week starting on %s\n", start.Format("2006/01/02"))
			for _, acc := range accounts {
				err := acc.GetEvents(w, &start, &end)
				if err != nil {
					fmt.Printf("Error on GetRentals for account %s : %s\n", acc.AccountName, err.Error())
				}
			}
		}
		time.Sleep(time.Duration(delaySecond) * time.Second)
	}

}

func eventlog(eventname string, payload interface{}) {
	fmt.Printf("Received event:%s\n", eventname)

}
