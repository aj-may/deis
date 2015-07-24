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
var inChannel chan *protocol
var outChannel chan *protocol

var regexpForPost = regexp.MustCompile(`^/([-a-z0-9]+)/.*`)
var regexpForGet = regexp.MustCompile(`^/([-a-z0-9]+)/([0-9]+)/.*`)

const TYPE_PUT = 0
const TYPE_REQUEST = 1

type protocol struct {
    Type int
    LinesNumber int
    ProjectName string
    Payload string
}

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

// Get specific amount of log lines for application name from main map with ring buffers
func getFromStorage(name string, lines int) string {
	currentRing, ok := logStorage[name]
	if !ok {
        return fmt.Sprintf("Could not find logs for project '%s'", name)
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
	return data
}

func RingBufferHandler(bufferSize int, webServicePort int) *Handler {
    inChannel = make(chan *protocol)
    outChannel = make(chan *protocol)
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
    go accessToStorage()
    var message = new(protocol)
    message.Type = TYPE_PUT
    var err error
	for {
		m := h.Get()
		if m == nil {
			break
		}
		if h.DrainURI != "" {
			drain.SendToDrain(m.String(), h.DrainURI)
		}
        message.Payload = m.String()
        message.ProjectName, err = getProjectName(message.Payload); if err != nil {
            message.Payload = fmt.Sprintln(err)
        }
        inChannel <- message
	}
	h.End()
}

// Actually only this function which should work in goroutines have access to log storage
func accessToStorage() {
    var message = new(protocol)
    for {
        message = <- inChannel
        switch message.Type {
            default: continue
            case TYPE_PUT:
                addToStorage(message.ProjectName, message.Payload)
            case TYPE_REQUEST:
                message.Payload = getFromStorage(message.ProjectName, message.LinesNumber)
                outChannel <- message
        }
    }
}

// Only one http handler which process requests
func httpHandler(w http.ResponseWriter, r *http.Request) {
    var message = new(protocol)
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
        message.Type = TYPE_PUT
        message.ProjectName = match[1]
        message.Payload = value[0]
        inChannel <- message
		return
	}

	match := regexpForGet.FindStringSubmatch(r.RequestURI)

	if match == nil {
		fmt.Fprintf(w, "Could not get application name from url: %s", r.RequestURI)
		return
	}

    logLines, err := strconv.Atoi(match[2])
	if err != nil {
		fmt.Fprintln(w, "Unable to get log lines parameter from request")
		return
	}
    message.Type = TYPE_REQUEST
    message.ProjectName = match[1]
    message.LinesNumber = logLines
    inChannel <- message
    message = <- outChannel

	if err != nil {
		fmt.Fprintln(w, err)
	} else {
        fmt.Fprint(w, message.Payload)
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
