package handlers

import (
	"container/ring"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/deis/deis/logger/drain"
	"github.com/deis/deis/logger/syslog"
)

var data string
var projectBuffer = make(map[string]*ring.Ring)
var ringBufferSize int

func addToBuffer(name string, message string) {
	currentRing, ok := projectBuffer[name]
	if !ok {
		r := ring.New(ringBufferSize)
		r.Value = message
		projectBuffer[name] = r
	} else {
		r := currentRing.Next()
		r.Value = message
		projectBuffer[name] = r
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

func (h *Handler) ringBufferLoop() {
	for {
		m := h.Get()
		if m == nil {
			break
		}
		if h.DrainURI != "" {
			drain.SendToDrain(m.String(), h.DrainURI)
		}
		err := writeToBuffer(m)
		if err != nil {
			log.Println(err)
		}
	}
	h.End()
}

func writeToBuffer(m syslog.SyslogMessage) error {
	appName, err := getProjectName(m.String())
	if err != nil {
		return err
	}
	addToBuffer(appName, m.String())
	return nil
}

func getString(line interface{}) {
	if line == nil {
		return
	}
	data += fmt.Sprintln(line)
}

func getFromBuffer(name string) (string, error) {
	currentRing, ok := projectBuffer[name]
	if !ok {
		return "", fmt.Errorf("Could not find logs for project '%s'", name)
	}
	data = ""
	currentRing.Next().Do(getString)
	return data, nil
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	regex := regexp.MustCompile(`^/([-a-z0-9]+)/.*`)
	match := regex.FindStringSubmatch(r.RequestURI)

	if match == nil {
		fmt.Fprintf(w, "Could not get application name from url: %s", r.RequestURI)
		return
	}
	data, err := getFromBuffer(match[1])
	if err != nil {
		fmt.Fprintln(w, err)
	} else {
		fmt.Fprint(w, data)
	}
}

func startWebService(port int) {
	http.HandleFunc("/", httpHandler)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Println(err)
	}
}
