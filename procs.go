package vbeam

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"

	"go.hasen.dev/vbeam/tsbridge"

	"go.hasen.dev/generic"
	"go.hasen.dev/vbolt"
)

const PREFIX_RPC = "/rpc/"
const PREFIX_DATA = "/data/"
const PREFIX_STATIC = "/static/"

type Context struct {
	AppName string // because same proc can be used by multiple applications
	Token   string
	*vbolt.Tx
}

type Application struct {
	Name       string
	Frontend   fs.FS
	StaticData fs.FS

	DB *vbolt.DB

	*http.ServeMux

	procMap  map[string]ProcedureInfo
	procList []string // keys into the procmap // TODO why do we have this list?!

	dataProcMap map[string]DataProcInfo
}

type Empty struct{}

var ErrorType = reflect.TypeOf((*error)(nil)).Elem()

var httpRequestPtr = reflect.TypeOf((*http.Request)(nil))

func ensurePatternHasSlashes(p *string) {
	if !strings.HasSuffix(*p, "/") {
		*p += "/"
	}
	if !strings.HasPrefix(*p, "/") {
		*p = "/" + *p
	}
}

func getCookieValue(req *http.Request, name string) (value string) {
	c, _ := req.Cookie(name)
	if c != nil && c.Name == name {
		value = c.Value
	}
	return
}

func MakeContext(app *Application, req *http.Request) (ctx Context) {
	ctx.AppName = app.Name
	ctx.Token = req.Header.Get("x-auth-token")
	// if no header, try cookies
	if ctx.Token == "" {
		ctx.Token = getCookieValue(req, "authToken")
	}
	if app.DB != nil {
		ctx.Tx = vbolt.ReadTx(app.DB)
	}
	return ctx
}

func CloseContext(ctx *Context) {
	vbolt.TxClose(ctx.Tx)
}

func UseWriteTx(ctx *Context) {
	if ctx.Tx == nil {
		return
	}
	if ctx.Tx.Writable() {
		return
	}
	db := ctx.Tx.DB()
	vbolt.TxClose(ctx.Tx)
	ctx.Tx = vbolt.WriteTx(db)
}

// NewApplication creates a new Application instance
func NewApplication(name string, db *vbolt.DB) *Application {
	app := new(Application)
	app.ServeMux = http.NewServeMux()
	generic.InitMap(&app.procMap)
	generic.InitMap(&app.dataProcMap)

	app.Name = name
	app.DB = db

	app.HandleFunc(PREFIX_RPC, app.HandleRPC)
	app.HandleFunc(PREFIX_DATA, app.HandleData)
	app.HandleFunc(PREFIX_STATIC, app.HandleStatic)
	app.HandleFunc("/", app.HandleRoot)

	return app
}

func (app *Application) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	app.ServeMux.HandleFunc(pattern, handler)
}

type ProcedureInfo struct {
	ProcValue reflect.Value
	ProcName  string

	// for typescript generation
	InputType  reflect.Type
	OutputType reflect.Type

	// for preventing malicious inputs
	MaxBytes int
}

// data procs are called at the address bar and return downloadable content
type DataProcInfo struct {
	ProcValue reflect.Value
	ProcName  string
	InputType reflect.Type
}

func WriteProcTSBinding(p *ProcedureInfo, w io.Writer) {
	var inputTypeName = p.InputType.Name()
	var outputTypeName = p.OutputType.Name()
	if p.InputType == httpRequestPtr {
		fmt.Fprintf(w, "export async function %s(data: BodyInit): Promise<rpc.Response<%s>> {\n", p.ProcName, outputTypeName)
		fmt.Fprintf(w, "    return await rpc.call<%s>('%s', data);\n", outputTypeName, p.ProcName)
		fmt.Fprintf(w, "}\n\n")

	} else {
		fmt.Fprintf(w, "export async function %s(data: %s): Promise<rpc.Response<%s>> {\n", p.ProcName, inputTypeName, outputTypeName)
		fmt.Fprintf(w, "    return await rpc.call<%s>('%s', JSON.stringify(data));\n", outputTypeName, p.ProcName)
		fmt.Fprintf(w, "}\n\n")
	}
}

func GenerateTSBindings(app *Application, targetFile string) {
	if targetFile == "" {
		fmt.Println("WARNING: targetFile not specified for", app.Name)
		return
	}

	log.Println("Writing RPC bindings:", targetFile)
	var f, err = os.OpenFile(targetFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fmt.Fprintf(f, `import * as rpc from "vlens/rpc"`)
	fmt.Fprintln(f)
	fmt.Fprintln(f)
	var s2t tsbridge.Bridge
	for _, procName := range app.procList {
		proc := app.procMap[procName]
		if proc.InputType != httpRequestPtr {
			s2t.QueueType(proc.InputType)
		}
		s2t.QueueType(proc.OutputType)
	}
	s2t.Process()
	tsbridge.WriteStructTSBinding(&s2t, f)
	for _, name := range app.procList {
		proc := app.procMap[name]
		WriteProcTSBinding(&proc, f)
	}
}

func _LocalProcName(procValue reflect.Value) string {
	fullName := runtime.FuncForPC(procValue.Pointer()).Name()
	dotIndex := strings.LastIndex(fullName, ".")
	procName := fullName[dotIndex+1:]
	return procName
}

func _RegisterProc(app *Application, proc any, maxBytes int) {
	var procValue = reflect.ValueOf(proc)
	var procType = procValue.Type()

	procName := _LocalProcName(procValue)

	var inputType reflect.Type = procType.In(1)

	var procInfo = ProcedureInfo{
		ProcValue:  procValue,
		ProcName:   procName,
		InputType:  inputType,
		OutputType: procType.Out(0),
		MaxBytes:   maxBytes,
	}
	app.procMap[procName] = procInfo
	app.procList = append(app.procList, procName)
}

func RegisterProc[Input, Output any](app *Application, proc func(*Context, Input) (Output, error)) {
	// 1MB is big enough for any json text. Use a file upload for larger requests
	_RegisterProc(app, proc, 1024*1024)
}

func RegisterProcRawInput[Output any](app *Application, proc func(*Context, *http.Request) (Output, error), maxBytes int) {
	_RegisterProc(app, proc, maxBytes)
}

func RegisterDataProc[Input any](app *Application, proc func(*Context, Input) (ContentDownload, error)) {
	_RegisterDataProc(app, proc)
}

func _RegisterDataProc(app *Application, proc any) {
	var procValue = reflect.ValueOf(proc)
	var procType = procValue.Type()

	procName := _LocalProcName(procValue)

	var inputType reflect.Type
	if procType.NumIn() > 0 {
		inputType = procType.In(1)
	}

	var procInfo = DataProcInfo{
		ProcValue: procValue,
		ProcName:  procName,
		InputType: inputType,
	}
	app.dataProcMap[procName] = procInfo
}
