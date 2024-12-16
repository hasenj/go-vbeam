package vbeam

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.hasen.dev/generic"
)

type Diagnostic struct {
	Type  string
	Begin bool

	Category  string
	Filename  string
	Line      int
	Character int
	Length    int
	Code      int
	Message   string
}

type TSReport struct {
	Time        time.Time
	Diagnostics []Diagnostic
}

//go:embed watch.mjs
var watchScript string

func TSWatch(dirlist []string, ch chan TSReport) {
	log.Println("TS Watch:", strings.Join(dirlist, " "))

	// install typescript if it's not installed
	if _, err := os.Stat("node_modules/typescript"); os.IsNotExist(err) {
		log.Println("Installing typescript library")
		cmd := exec.Command("npm", "install", "--save-dev", "typescript@5.x")
		if err := cmd.Run(); err != nil {
			log.Println("Failed to install TypeScript:", err)
			close(ch)
			return
		}
	}

	args := append([]string{"--input-type=module", "-"}, dirlist...)
	cmd := exec.Command("node", args...)

	stdin := generic.Must(cmd.StdinPipe())
	stdout := generic.Must(cmd.StdoutPipe())
	stderr := generic.Must(cmd.StderrPipe())

	io.WriteString(stdin, watchScript)
	stdin.Close()

	if err := cmd.Start(); err != nil {
		log.Println("watch error!!", err)
		return
	}

	generic.AddExitCleanup(func() {
		cmd.Process.Kill()
	})

	log.Println("Watch started!!")

	var report TSReport

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var diagnostic Diagnostic
		line := scanner.Bytes()
		json.Unmarshal(line, &diagnostic)
		switch diagnostic.Type {
		case "report":
			if diagnostic.Begin {
				// send the previous report with the zero tiemstamp
				// to indicate we are error-checking, but while keeping
				// the current list of errors until the new check completes
				report.Time = time.Time{}
				ch <- report

				// start the new report ..
				report = TSReport{}
				report.Time = time.Now()

			} else {
				// report end, print results
				if ch == nil {
					PrintReport(report)
				} else {
					ch <- report
				}
			}
		case "diagnostic":
			generic.Append(&report.Diagnostics, diagnostic)
		}
	}

	// tswatch program exited!!
	close(ch)

	// report the error from stderr to the user
	// probably will not be very helpful, but it's better than nothing!
	var output strings.Builder
	io.Copy(&output, stderr)

	if err := cmd.Wait(); err != nil {
		log.Println("Type Checker:", err)
		if output.Len() > 0 {
			log.Println(output.String())
		}
		return
	}
}

const startBold = "\x1B[1m"
const endBold = "\x1B[0m"

const startDim = "\x1B[2m"
const endDim = "\x1B[22m"

const startRed = "\x1B[31m"
const startYellow = "\x1B[33m"
const startGreen = "\x1B[32m"
const startGray = "\x1B[90m"
const endColor = "\x1B[39m"

const startBgLightRed = "\x1B[41m"
const startBgYellow = "\x1B[43m"
const startBgGray = "\x1B[100m"
const endBgColor = "\x1B[49m"

var prevErrors int

func PrintReport(report TSReport) {
	b := &strings.Builder{}
	// allows early exit
	defer func() {
		prevErrors = len(report.Diagnostics)
		if b.Len() > 0 { // if we have something to report ..
			log.Print(b.String())
		}
	}()

	if len(report.Diagnostics) == 0 {
		if prevErrors != 0 {
			b.WriteString(startBold + startGreen + "No Typescript Errors" + endColor + endBold)
		}
		return
	}

	fmt.Fprintf(b, "%s%d errors%s\n", startRed, len(report.Diagnostics), endColor)
	for i, diag := range report.Diagnostics {
		fmt.Fprintf(b, "%s%s:%d:%d%s\n", startBold, diag.Filename, diag.Line, diag.Character, endBold)
		if i <= 3 {
			PrintTSDiagnosticQuote(b, diag)
		}
		b.WriteString(startBgGray)
		b.WriteString(diag.Message)
		b.WriteString(endBgColor)
		b.WriteRune('\n')
		b.WriteRune('\n')
	}
}

func PrintTSDiagnosticQuote(b *strings.Builder, diag Diagnostic) {
	var fq FileQuote
	fq.Filename = diag.Filename
	fq.Line = diag.Line
	fq.Character = diag.Character
	fq.Read(2, 2)

	for _, line := range fq.SurroundingLines {
		b.WriteString(startDim)
		b.WriteString(startGray)
		fmt.Fprintf(b, " %4d |", line.Number)
		b.WriteString(endColor)
		b.WriteString(endDim)

		if line.Number == fq.Line {
			startIndex := diag.Character - 1
			endIndex := startIndex + diag.Length

			runes := ([]rune)(line.Text)
			before := runes[:startIndex]
			within := runes[startIndex:endIndex]
			after := runes[endIndex:]

			b.WriteString(startBgGray)
			b.WriteString(string(before))
			b.WriteString(endBgColor)

			b.WriteString(startBgLightRed)
			b.WriteString(string(within))
			b.WriteString(endBgColor)

			b.WriteString(startBgGray)
			b.WriteString(string(after))
			b.WriteString(endBgColor)
			b.WriteRune('\n')

			/*
				// gutter
				fmt.Fprintf(b, "%s      |%s", startDim, endDim)
				fmt.Fprint(b, startRed)
				for i := range line.Text {
					if i >= firstChar && i < firstChar+diag.Length {
						fmt.Fprintf(b, "^")
					} else {
						fmt.Fprintf(b, " ")
					}
				}
				fmt.Fprint(b, endColor)
				fmt.Fprintln(b)
			*/
		} else {
			b.WriteString(startDim)
			fmt.Fprintln(b, line.Text)
			b.WriteString(endDim)
		}

	}
}
