package vbeam

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/fatih/color"
	"go.hasen.dev/generic"
)

type FileLine struct {
	Number int
	Text   string
}

type StackTraceElement struct {
	Package  string // prefix of function
	Function string
	FileQuote
}

type FileQuote struct {
	Filename         string
	Line             int
	Character        int // 0 for unspecified, 1-index based
	SurroundingLines []FileLine
}

func (fq *FileQuote) Read(countBefore int, counteAfter int) {
	generic.ResetSlice(&fq.SurroundingLines)
	var startLine = fq.Line - (countBefore + 1)
	if startLine < 0 {
		startLine = 0
	}
	var endLine = fq.Line + counteAfter
	var f, ferr = os.Open(fq.Filename)
	if ferr != nil {
		return
	}
	defer f.Close()

	var reader = bufio.NewReader(f)
	var currLine = 0
	for currLine < startLine {
		_, rerr := reader.ReadString('\n')
		if rerr != nil {
			return
		}
		currLine++
	}
	for currLine < endLine {
		line, rerr := reader.ReadString('\n')
		if rerr != nil {
			break // don't continue framesLoop here; we need to cleanup so just exit the loop
		}
		line = strings.TrimSuffix(line, "\n")
		currLine++
		generic.Append(&fq.SurroundingLines, FileLine{currLine, line})
	}
	return
}

func packageSplit(fn string) (string, string) {
	var slashIndex = strings.LastIndex(fn, "/")
	// fmt.Printf("last index of slash in %s is %d\n", fn, slashIndex)
	var dotOffset = strings.Index(fn[slashIndex+1:], ".")
	if dotOffset == -1 {
		return "", fn
	}
	var splitAt = slashIndex + 1 + dotOffset
	return fn[:splitAt], fn[splitAt:]
}

func UsefulStackTrace() []StackTraceElement {
	var cwd, _ = os.Getwd()
	var trace = make([]StackTraceElement, 0, 20)
	var callers = make([]uintptr, 20)
	var count = runtime.Callers(0, callers)
	callers = callers[:count]
	var frames = runtime.CallersFrames(callers)
	var frame runtime.Frame
	var hasMore = true

	const skipTo = "runtime.gopanic"
	var skipping = true

	for hasMore {
		frame, hasMore = frames.Next()
		var element StackTraceElement
		element.Function = frame.Function
		element.Filename = frame.File
		element.Line = frame.Line

		if skipping {
			if element.Function == skipTo {
				skipping = false
			}
			continue
		}

		var ourCode = strings.HasPrefix(element.Filename, cwd)
		if ourCode {
			element.FileQuote.Read(4, 4)
		}
		element.Package, _ = packageSplit(frame.Function)
		trace = append(trace, element)
	}
	return trace
}

var warningRed = color.New(color.FgRed, color.Bold)
var warningColor = color.New(color.FgYellow, color.Bold)
var infoBlue = color.New(color.FgBlue)

var codeColor = color.New(color.FgHiMagenta)
var codeErrorColor = color.New(color.FgRed)
var gutterGray = color.New(color.FgWhite, color.BgHiBlack)

func PrintUsefulStackTrace(buf io.Writer) {
	var trace = UsefulStackTrace()
	PrintStacktraceElements(buf, trace)
}

func PrintStacktraceElements(buf io.Writer, trace []StackTraceElement) {
	for _, element := range trace {
		fmt.Fprint(buf, "    ")
		warningColor.Fprintln(buf, element.Function)

		PrintFileQuote(buf, element.FileQuote)
		fmt.Fprintln(buf)
	}
}

func PrintFileQuote(buf io.Writer, fq FileQuote) {
	fmt.Fprint(buf, "    ")
	infoBlue.Fprintf(buf, "%s:%d", fq.Filename, fq.Line)
	if fq.Character > 0 {
		infoBlue.Fprintf(buf, ":%d", fq.Character)
	}
	fmt.Fprintln(buf)
	for _, line := range fq.SurroundingLines {
		fmt.Fprint(buf, "    ")
		gutterGray.Fprintf(buf, "   %4d ", line.Number)
		if line.Number == fq.Line {
			codeErrorColor.Fprint(buf, line.Text)
		} else {
			codeColor.Fprint(buf, line.Text)
		}
		fmt.Println(buf)
		fmt.Println(buf)
	}
}

func NiceStackTraceOnPanic() {
	err := recover()
	if err != nil {
		log.Println("Panic!!!")
		log.Println(err)
		var buf = new(bytes.Buffer)
		PrintUsefulStackTrace(buf)
		io.Copy(os.Stderr, buf)
	}
}
