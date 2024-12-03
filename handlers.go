package vbeam

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"go.hasen.dev/generic"
)

// ResponseWriter helps us capture the statusCode that was written
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int

	// how much time did we spend inside the handler procDur
	procDur time.Duration
}

func WrapHttpResponeWriter(w http.ResponseWriter) *ResponseWriter {
	wrapped, ok := w.(*ResponseWriter)
	if !ok {
		wrapped = &ResponseWriter{ResponseWriter: w}
	}
	return wrapped
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func RespondError(w http.ResponseWriter, err error) {
	w.WriteHeader(400)
	fmt.Fprintf(w, err.Error())
}

func ServerTimingHeaderValue(dur time.Duration) string {
	return fmt.Sprintf("proc;dur=%f", float64(dur.Microseconds())/1000.0)
}

func Respond(w *ResponseWriter, object interface{}) {
	var t = reflect.TypeOf(object)
	header := w.Header()

	header.Set("Server-Timing", ServerTimingHeaderValue(w.procDur))

	if t.Kind() == reflect.Struct { // TODO should we handle maps the same way?
		header.Set("Content-Type", "application/json")
		var enc = json.NewEncoder(w)
		enc.Encode(object)
		return
	}

	if s, ok := object.(fmt.Stringer); ok {
		header.Set("Content-Type", "text/plain")
		fmt.Fprint(w, s.String())
		return
	}

	if str, ok := object.(string); ok {
		header.Set("Content-Type", "text/plain")
		fmt.Fprint(w, str)
		return
	}

	/*
		if s, ok := object.(io.Writer); ok {
			// TODO: should we really set it to plain/text? What if it's a different file?
			header.Set("Content-Type", "text/plain")
			ctx.Write(s)
			return
		}
	*/

	w.WriteHeader(500)
	fmt.Fprintf(w, "INTERNAL PROGRAMMING ERROR")
	// panic(errors.New(fmt.Sprintf("Don't know how to serve %#v", object)))
}

type ContentDownload struct {
	ContentType string
	Filename    string
	WriteTo     func(w *bufio.Writer)
}

func RespondContentDownload(w *ResponseWriter, content *ContentDownload) {
	header := w.Header()
	header.Set("Server-Timing", ServerTimingHeaderValue(w.procDur))
	header.Set("Content-Type", content.ContentType)
	header.Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", content.Filename))
	writer := bufio.NewWriterSize(w, 4096*16)
	content.WriteTo(writer)
	writer.Flush()
}

func DigitsIn(n int) int {
	if n == 0 {
		return 1
	}

	count := 0
	for n > 0 {
		count++
		n /= 10
	}
	if n < 0 {
		count++
	}
	return count
}

var faintColor = color.New(color.FgHiBlack, color.Faint)

// this function is meant to be deferred
// print times and recover panics
func postProcess(w *ResponseWriter, request *http.Request, start time.Time) {
	var duration = time.Now().Sub(start)
	var code = w.statusCode
	if code == 0 {
		code = 200
	}

	remoteAddr := request.RemoteAddr
	if strings.HasPrefix(remoteAddr, "[::1]:") { // proxy!
		// fmt.Println(request.Header.Write(os.Stdout))
		remoteAddr = request.Header.Get("X-Forwarded-For")
	}

	var buf strings.Builder

	// handle panics first - we can't assume by default things went ok
	if crash := recover(); crash != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Server Error")
		warningRed.Fprint(&buf, "\n")
		warningRed.Fprint(&buf, "=======================================\n")
		warningRed.Fprint(&buf, "   ******* Handler panicked! *******   \n")
		warningRed.Fprint(&buf, "---------------------------------------\n")
		warningRed.Fprintf(&buf, "%s %s %s\n", request.Method, request.Host, request.RequestURI)
		warningRed.Fprint(&buf, "---------------------------------------\n")
		warningRed.Fprintf(&buf, "%v\n", crash)
		PrintUsefulStackTrace(&buf)
		warningRed.Fprint(&buf, "=======================================\n")
	} else {
		fmt.Fprintf(&buf, "%-20s %d %-4s %s", remoteAddr, code, request.Method, request.RequestURI)
		microSeconds := int(duration.Microseconds())
		const maxLength = 40
		padding := maxLength - (len(request.RequestURI) + DigitsIn(microSeconds))
		fmt.Fprintf(&buf, " ")
		for i := 0; i < padding; i++ {
			faintColor.Fprint(&buf, "⎯")
		}
		fmt.Fprintf(&buf, " ")
		fmt.Fprint(&buf, microSeconds)
		fmt.Fprint(&buf, "µs")
		if w.procDur > 0 {
			fmt.Fprintf(&buf, " [")
			fmt.Fprint(&buf, w.procDur.Microseconds())
			fmt.Fprint(&buf, "µs]")
		}

	}

	log.Print(buf.String())
}

func (app *Application) ServeHTTP(wp http.ResponseWriter, request *http.Request) {
	start := time.Now()
	var w = WrapHttpResponeWriter(wp)
	defer postProcess(w, request, start)

	app.ServeMux.ServeHTTP(w, request)
}

func ModifiedRequestPath(req *http.Request, npath string) *http.Request {
	nreq := new(http.Request)
	*nreq = *req
	nreq.URL = new(url.URL)
	nreq.URL.Path = npath
	return nreq
}

var extensionRegex = regexp.MustCompile(`\.[a-zA-Z]{1,4}$`)

func isExt(path string) bool {
	return extensionRegex.MatchString(path)
}

func (app *Application) HandleRoot(w http.ResponseWriter, request *http.Request) {
	serveSPA(app.Frontend, w, request)
}

func serveSPA(frontend fs.FS, w http.ResponseWriter, r *http.Request) {
	server := http.FileServer(http.FS(frontend))

	path := r.URL.Path
	_, err := fs.Stat(frontend, strings.TrimPrefix(path, "/"))

	if strings.HasSuffix(path, "/") || (os.IsNotExist(err) && !isExt(path)) {
		r = ModifiedRequestPath(r, "/")
	}
	if err == nil {
		w.Header().Set("Cache-Control", "max-age=86400") // 24 hours in seconds
	}

	server.ServeHTTP(w, r)
}

var ProcedureNotFound = errors.New("Procedure Not Found")

func (app *Application) HandleRPC(w http.ResponseWriter, request *http.Request) {
	if request.Method != "POST" {
		RespondError(w, errors.New("RPC calls must be POST"))
		return
	}

	var procName = strings.TrimPrefix(request.RequestURI, PREFIX_RPC)
	var proc, found = app.procMap[procName]
	if !found {
		RespondError(w, ProcedureNotFound)
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, int64(proc.MaxBytes))

	var output []reflect.Value

	var procStart time.Time
	if proc.InputType == httpRequestPtr { // non-json body
		procStart = time.Now()
		// let the proc process its own input
		func() { // Go version of a scoped defer
			var context = MakeContext(app, request)
			defer CloseContext(&context)
			var args = []reflect.Value{
				reflect.ValueOf(&context),
				reflect.ValueOf(request),
			}
			output = proc.ProcValue.Call(args)
		}()
	} else { // json body
		var decoder = json.NewDecoder(request.Body)
		// parse the input json into a struct
		var requestObject = reflect.New(proc.InputType)
		var err = decoder.Decode(requestObject.Interface())
		if err != nil {
			fmt.Println("error decoding request json")
			RespondError(w, errors.New("InvalidRequest"))
			return
		}
		procStart = time.Now()
		func() { // Go version of a scoped defer
			var context = MakeContext(app, request)
			defer CloseContext(&context)

			var args = []reflect.Value{
				reflect.ValueOf(&context),
				requestObject.Elem(),
			}
			output = proc.ProcValue.Call(args)
		}()
	}

	rw := w.(*ResponseWriter)
	rw.procDur = time.Since(procStart)
	// check if error was returned
	if output[1].IsNil() {
		Respond(rw, output[0].Interface())
	} else {
		var err = output[1].Interface().(error)
		RespondError(w, err)
	}
}

func (app *Application) HandleStatic(w http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		http.Error(w, "Only GET requests supported", 400)
		return
	}
	var uri, err = url.PathUnescape(request.RequestURI)
	if err != nil {
		http.Error(w, "Path invalid", 400)
		return
	}
	var newPath = strings.TrimPrefix(uri, "/static")
	if strings.HasSuffix(newPath, "/") {
		http.Error(w, "Forbidden", 403)
		return
	}

	newRequest := ModifiedRequestPath(request, newPath)

	// cache for 24 hours
	w.Header().Set("Cache-Control", "max-age=86400")

	staticServer := http.FileServer(http.FS(app.StaticData))
	staticServer.ServeHTTP(w, newRequest)
}

func (app *Application) HandleData(w http.ResponseWriter, request *http.Request) {
	// unlike the RPC, the data url requires a GET request, and results in
	// some kind of file download
	if request.Method != "GET" {
		http.Error(w, "Only GET requests supported", 400)
		return
	}
	var procName = strings.TrimPrefix(request.RequestURI, PREFIX_DATA)
	var proc, found = app.dataProcMap[procName]
	if !found {
		RespondError(w, ProcedureNotFound)
		return
	}

	var requestObject = reflect.New(proc.InputType)
	// TODO: parse input from get params
	/*
		var err = decoder.Decode(requestObject.Interface())
		if err != nil {
			fmt.Println("error decoding get parameters")
			RespondError(w, errors.New("InvalidRequest"))
			return
		}
	*/

	var output []reflect.Value
	procStart := time.Now()
	func() { // Go version of a scoped defer
		var context = MakeContext(app, request)
		defer CloseContext(&context)

		var args = []reflect.Value{
			reflect.ValueOf(&context),
			requestObject.Elem(),
		}

		output = proc.ProcValue.Call(args)
	}()

	rw := w.(*ResponseWriter)
	rw.procDur = time.Since(procStart)

	// check if error was returned
	if output[1].IsNil() {
		content := output[0].Interface().(ContentDownload)
		RespondContentDownload(rw, &content)
	} else {
		var err = output[1].Interface().(error)
		RespondError(w, err)
	}
}

// ------------------------------------------
// section: Back server
// ------------------------------------------
//
// The "back" server is just a goroutine that listens on a specific port that
// allows us to gracefully "terminate" the running instnace of the same program
// so that it releases the database and the tcp port for the http server
//

func readerToString(r io.Reader) string {
	var buf strings.Builder
	io.Copy(&buf, r)
	return buf.String()
}

func RunBackServer(port int) {
	if port <= 0 {
		return
	}

	// IMPORTANT: This should only accept connections from localhost.
	// This is why we use 127.0.0.1
	Addr := fmt.Sprintf("127.0.0.1:%d", port)

	exitWaitTimeOld := time.Millisecond * 100
	exitWaitTimeNew := time.Millisecond * 200

	// TODO: make this a plain TCP server instead of HTTP
	// first, send a request to existing back server to kill itself!
	{
		resp, err := http.Post("http://"+Addr+"/cmd", "text/plain", strings.NewReader("terminate"))
		if err == nil {
			resp, _ := io.ReadAll(resp.Body)
			_ = resp // drain response body
			// log.Println("terminate command response:", string(resp))
			// } else {
			// log.Println("terminate command error:", err)
		}

		time.Sleep(exitWaitTimeNew) // wait for it
	}

	var mux = http.NewServeMux()
	server := &http.Server{Addr: Addr, Handler: mux}

	mux.HandleFunc("/cmd", func(w http.ResponseWriter, req *http.Request) {
		var cmd = readerToString(req.Body)
		log.Println("Back server command received:", cmd)
		if cmd == "terminate" {
			log.Println()
			log.Printf(" [%d] Terminating!\n\n", os.Getpid())

			io.WriteString(w, "closed")
			// server.Close()

			// fire a gothrough to exit us in a second
			go func() {
				log.Println("Back server got terminate command!")
				time.Sleep(exitWaitTimeOld)
				generic.ExitWithCleanup(0)
			}()
		}
	})

	go server.ListenAndServe()
}
