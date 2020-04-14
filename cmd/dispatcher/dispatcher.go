package dispatcher

import (
	"errors"
	"strings"
	"sync"
)

//Callback is the function prototype Subscribers have to implement
type Callback func(eventname string, p interface{})

//Channel is the place to register to or to broadcast events
type Channel struct {
	subscribers map[string][]Callback
	mutex       sync.Mutex
}

//Channels keep a list of created channels. Just for info
var Channels map[string]*Channel

//CreateChannel creates and init a channel
func CreateChannel(name string) (*Channel, error) {
	c := Channel{}
	c.Init()
	if len(name) > 0 {
		if Channels == nil {
			Channels = make(map[string]*Channel)
		}
		if _, ok := Channels[name]; ok {
			return nil, errors.New("Channel already exists")
		}
		Channels[name] = &c
	}
	return &c, nil
}

//Init initialize the channel map
func (c *Channel) Init() {
	c.subscribers = make(map[string][]Callback)

}

//Publish send an event to subscribers
func (c *Channel) Publish(eventname string, payload interface{}) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for k, sub := range c.subscribers {
		if strings.HasPrefix(eventname, k) {
			for _, c := range sub {
				go c(eventname, payload)
			}
		}
	}
}

//Subscribe : listen to events
func (c *Channel) Subscribe(eventname string, call Callback) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	subs, ok := c.subscribers[eventname]
	if !ok {
		c.subscribers[eventname] = []Callback{call}
		return
	}
	c.subscribers[eventname] = append(subs, call)
}
