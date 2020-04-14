package tools

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marcmorel/morm"
)

/*RandomHex provides a random hexa string of n length */
func RandomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

//ConvertStringToAmount function converts a string amount into an int64 of CENTS.
//we use that trick to avoid any float in the app (and rounding issues)
// " - 1234,43 " --> -123443
// "0" 			 --> 0
// "345 "  		 --> 34500
// " 345,1"		 --> 3451
// "- 0,00"		 --> 0
// "345,"		 --> 3450
//
func ConvertStringToAmount(s string, decimalSep string, thousandSep string) (int64, error) {
	s = strings.ReplaceAll(s, " ", "")
	if thousandSep != "" {
		s = strings.ReplaceAll(s, thousandSep, "")
	}
	l := len(s)
	sepPos := strings.LastIndex(s, decimalSep)
	if sepPos != -1 {
		s = strings.ReplaceAll(s, decimalSep, "")
	}
	switch sepPos {
	case -1: //int number
		s += "00"
	case l - 1: //decimal separator was on last char
		s += "00" //x100
	case l - 2: //one digit only for the decimal part
		s += "0"
	case l - 3: //2digits:
		//nothing to do
	default:
		//more than 2 digits after the decimal point. Weird, let's cut the decimal part after 2 digits
		s = s[0 : sepPos+2]
		fmt.Printf("Apres %s\n", s)
	}

	//still here ? we now have  a string without spaces and separators.
	return strconv.ParseInt(s, 10, 64)
}

//ConvertAmountToString function converts a  amount of CENTS into a real string
func ConvertAmountToString(a int64, decimalSep string) string {
	f := float64(a) / 100
	result := fmt.Sprintf("%.2f", f)
	if decimalSep != "." {
		result = strings.ReplaceAll(result, ".", decimalSep)
	}
	return result
}

//constants to interpret month prorata calculation
const (
	ProrataStrict       = 0
	ProrataEndOfMonth   = 1
	ProrataBeginOfMonth = 2
	ProrataBoth         = 3
)

//GetMonthProrata returns a float representing the number of monthes between two dates
//with the rounding rules given above
func GetMonthProrata(startsAt time.Time, endsAt time.Time, mode int) float32 {
	monthBeginning := time.Date(startsAt.Year(), startsAt.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnding := time.Date(endsAt.Year(), endsAt.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if mode == ProrataEndOfMonth || mode == ProrataBoth {
		if endsAt.Day() != 1 {
			endsAt = monthEnding

		}
	}
	if mode == ProrataBeginOfMonth || mode == ProrataBoth {
		if startsAt.Day() != 1 {
			startsAt = monthBeginning
		}
	}

	//whole monthes only :
	if startsAt.Day() == 1 && endsAt.Day() == 1 {
		prorata := float32(int(endsAt.Month()) + endsAt.Year()*12 - (int(startsAt.Month()) + startsAt.Year()*12))
		return prorata
	}

	prorataStart := float32(0.00)
	if startsAt.Day() != 1 {
		//days of month:
		firstDayOfNextMonth := time.Date(startsAt.Year(), startsAt.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		days := firstDayOfNextMonth.AddDate(0, 0, -1).Day()
		prorataStart = float32(days+1-startsAt.Day()) / float32(days)
		startsAt = firstDayOfNextMonth
	}
	prorataEnd := float32(0.00)
	if endsAt.Day() != 1 {
		//days of month:
		days := time.Date(endsAt.Year(), endsAt.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).Day()
		prorataEnd = float32(endsAt.Day()-1) / float32(days)

		endsAt = time.Date(endsAt.Year(), endsAt.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	prorata := float32(int(endsAt.Month()) + endsAt.Year()*12 - (int(startsAt.Month()) + startsAt.Year()*12))
	prorata += prorataEnd + prorataStart

	return float32(math.Round(float64(prorata*100)) / 100.0)
}

// Unzip will decompress a zip archive, moving all files. No folders will be created
// within the zip file (parameter 1) to an output directory (parameter 2).
func Unzip(src string, dest string) ([]string, error) {

	var filenames []string
	r, err := zip.OpenReader(src)
	if err != nil {
		return filenames, err
	}
	defer r.Close()

	for _, f := range r.File {
		// Store filename/path for returning and using later on
		fpath := filepath.Join(dest, f.Name)

		// Check for ZipSlip. More Info: http://bit.ly/2MsjAWE
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return filenames, fmt.Errorf("%s: illegal file path", fpath)
		}

		filenames = append(filenames, fpath)

		if f.FileInfo().IsDir() {
			// Make Folder
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return filenames, err
			}
			continue
		}
		// Make File
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return filenames, err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return filenames, err
		}

		rc, err := f.Open()
		if err != nil {
			return filenames, err
		}

		_, err = io.Copy(outFile, rc)
		// Close the file without defer to close before next iteration of loop
		outFile.Close()
		rc.Close()

		if err != nil {
			return filenames, err
		}
	}
	return filenames, nil
}

//ExtractEncodedFields is used to filter and safestring a data map got from a query
func ExtractEncodedFields(param map[string]string, data map[string]interface{}) map[string]string {
	result := map[string]string{}
	for dest, source := range param {

		result[dest] = morm.SafeString(data[source])

	}
	return result
}

//ExtractEncodedArrayFields is used to filter and safestring an array of data map got from a query
func ExtractEncodedArrayFields(param map[string]string, data []map[string]interface{}) []map[string]string {

	result := make([]map[string]string, len(data))
	for i, m := range data {
		result[i] = ExtractEncodedFields(param, m)
	}
	return result
}
