package handler

import (
	"container/ring"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"

	"github.com/deis/deis/logger/drain"
	"github.com/deis/deis/logger/syslog"
)

var logStorage = make(map[string]*ring.Ring)
var ringBufferSize int

var regexpForPost = regexp.MustCompile(`^/([-a-z0-9]+)/.*`)
var regexpForGet = regexp.MustCompile(`^/([-a-z0-9]+)/([0-9]+)/.*`)

// Add log message to main map with ring byffers by project name
func addToStorage(name string, message string) {
	currentRing, ok := logStorage[name]
	if !ok {
		r := ring.New(ringBufferSize)
		r.Value = message
		logStorage[name] = r
	} else {
		r := currentRing.Next()
		r.Value = message
		logStorage[name] = r
	}
}

func RingBufferHandler(bufferSize int, webServicePort int) *Handler {
	ringBufferSize = bufferSize
	h := Handler{
		BaseHandler: syslog.NewBaseHandler(5, filter, false),
	}
	go h.ringBufferLoop()
	go startWebService(webServicePort)
	fmt.Printf("Web service started on %d port\n", webServicePort)
	return &h
}

// Main loop for this handler, each log line will be sended to drain (if DrainURI specified) and copied to log storage
func (h *Handler) ringBufferLoop() {
	for {
		m := h.Get()
		if m == nil {
			break
		}
		if h.DrainURI != "" {
			drain.SendToDrain(m.String(), h.DrainURI)
		}
		if err := writeToStorage(m); err != nil {
			log.Println(err)
		}
	}
	h.End()
}
// Tring to get application name from message and write log line to log storage
func writeToStorage(m syslog.SyslogMessage) error {
	appName, err := getProjectName(m.String())
	if err != nil {
		return err
	}
	addToStorage(appName, m.String())
	return nil
}

// Get specific amount of log lines for application name from main map with ring buffers
func getFromStorage(name string, lines int) (string, error) {
	currentRing, ok := logStorage[name]
	if !ok {
		return "", fmt.Errorf("Could not find logs for project '%s'", name)
	}
	var data string
	getLine := func(line interface{}) {
		if line == nil || lines <= 0 {
			return
		}
		lines -= 1
		data += fmt.Sprintln(line)
	}

	currentRing.Next().Do(getLine)
	return data, nil
}

// Only one http handler which process requests
func httpHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method == "POST" {
		match := regexpForPost.FindStringSubmatch(r.RequestURI)

		if match == nil {
			fmt.Fprintf(w, "Could not get application name from url: %s", r.RequestURI)
			return
		}

		r.ParseForm()
		value, ok := r.Form["message"]
		if !ok {
			fmt.Fprintln(w, "Could not read from post request, no 'message' param in POST")
			return
		}
		addToStorage(match[1], value[0])
		return
	}

	match := regexpForGet.FindStringSubmatch(r.RequestURI)

	if match == nil {
		fmt.Fprintf(w, "Could not get application name from url: %s", r.RequestURI)
		return
	}

	log_lines, err := strconv.Atoi(match[2])
	if err != nil {
		fmt.Fprintln(w, "Unable to get log lines parameter from request")
		return
	}
	data, err := getFromStorage(match[1], log_lines)
	if err != nil {
		fmt.Fprintln(w, err)
	} else {
		fmt.Fprint(w, data)
	}
}

// Start web service which serve controller request for get and post logs
func startWebService(port int) {
	http.HandleFunc("/", httpHandler)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Println(err)
	}
}
