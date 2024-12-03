package esbuilder

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/fatih/color"
	"github.com/fsnotify/fsnotify"
	"github.com/otiai10/copy"
	"go.hasen.dev/generic"
)

type FEBuildOptions struct {
	NoSourceMaps bool

	FERoot string

	// relative to FERoot
	EntryTS []string

	Define map[string]string
	Outdir string

	// relative to FERoot
	EntryHTML []string

	// relative to FERoot
	CopyItems []string // files and directories to copy as-is to the output directory
}

// TODO: use the same structure for TS Report
type ESReport struct {
	Time     time.Time
	Duration time.Duration

	Done bool

	// TODO: use the same structure as the ts diagnostics
	Errors   []api.Message
	Messages []string
}

func FEBuild(options FEBuildOptions, ch chan ESReport) bool {
	t0 := time.Now()

	var report ESReport
	report.Time = t0
	if ch != nil {
		// indicate the start of building
		ch <- report
	}

	esEntryPoints := make([]string, 0, len(options.EntryTS))
	for _, entryPoint := range options.EntryTS {
		// log.Println("entry:", entryPoint)
		generic.Append(&esEntryPoints, path.Join(options.FERoot, entryPoint))
	}

	esbOptions := api.BuildOptions{
		Format:            api.FormatESModule,
		EntryPoints:       esEntryPoints,
		EntryNames:        "[dir]/[name]-[hash]",
		Charset:           api.CharsetUTF8,
		Bundle:            true,
		Metafile:          true,
		Splitting:         true,
		TreeShaking:       api.TreeShakingTrue,
		Outdir:            options.Outdir,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Sourcemap:         api.SourceMapLinked,
		LogLimit:          3,
		JSXFactory:        "preact.h",
		JSXFragment:       "preact.Fragment",
		Define:            options.Define,
		Write:             true,
	}

	if options.NoSourceMaps {
		esbOptions.Sourcemap = api.SourceMapNone
	}

	os.RemoveAll(options.Outdir)
	cleanTime := time.Since(t0).Round(time.Millisecond)
	_ = cleanTime

	t1 := time.Now()
	result := api.Build(esbOptions)

	buildTime := time.Since(t1).Round(time.Millisecond)
	_ = buildTime

	report.Errors = result.Errors

	if len(result.Errors) > 0 {
		report.Done = true
		report.Time = time.Now()
		report.Duration = report.Time.Sub(t0)

		if ch != nil {
			ch <- report
		} else {
			// FIXME: eventually we don't want to print anything here; just send
			// the report over the channel and let the receiving end print
			msgColor := color.New(color.FgRed, color.Bold)
			locColor := color.New(color.FgMagenta)
			contentColor := color.New(color.FgHiCyan)
			contentErrorColor := color.New(color.FgHiRed)
			errorCursorColor := color.New(color.FgHiRed)

			for _, err := range result.Errors {
				if err.Location != nil {
					locColor.Printf("%s:%d:%d: ", err.Location.File, err.Location.Line, err.Location.Column)
				}
				msgColor.Println(err.Text)
				if err.Location != nil {
					fmt.Println()
					fmt.Print("    ")
					textBefore := err.Location.LineText[:err.Location.Column]
					textError := err.Location.LineText[err.Location.Column : err.Location.Column+err.Location.Length]
					textAfter := err.Location.LineText[err.Location.Column+err.Location.Length:]

					contentColor.Print(textBefore)
					contentErrorColor.Print(textError)
					contentColor.Print(textAfter)
					fmt.Println()
					fmt.Print("    ")
					for range textBefore {
						fmt.Print(" ")
					}
					for range textError {
						errorCursorColor.Print("^")
					}

					fmt.Println()
				}
			}
		}

		return false
	}

	t2 := time.Now()

	// get the mapping of entry points to output files
	{
		type EntryOutput struct {
			EntryPath  string
			OutputPath string
		}

		// map the supposed js path to the actual output path
		var entryOutputs []EntryOutput

		// we don't care for now
		type MetaInput struct{}
		type MetaOutput struct {
			EntryPoint string `json:"entryPoint"`
		}
		type MetaContent struct {
			Inputs  map[string]MetaInput  `json:"inputs"`
			Outputs map[string]MetaOutput `json:"outputs"`
		}

		var meta MetaContent
		json.Unmarshal([]byte(result.Metafile), &meta)

		for key, value := range meta.Outputs {
			if value.EntryPoint != "" {
				entryPath := value.EntryPoint
				outputPath := key

				entryPath = strings.TrimPrefix(entryPath, options.FERoot)
				if !strings.HasPrefix(entryPath, "/") {
					entryPath = "/" + entryPath
				}
				outputPath = strings.TrimPrefix(outputPath, options.Outdir)
				if !strings.HasPrefix(outputPath, "/") {
					outputPath = "/" + outputPath
				}
				generic.Append(&entryOutputs, EntryOutput{EntryPath: entryPath, OutputPath: outputPath})
				// log.Println(entryPath, "=>", outputPath)
			}
		}

		// process entry html files
		for _, htmlFile := range options.EntryHTML {
			contentBytes, err := os.ReadFile(path.Join(options.FERoot, htmlFile))
			if err != nil {
				generic.Append(&report.Messages,
					fmt.Sprintf("Error reading entry html file %s: %s",
						htmlFile,
						err))
			}
			content := generic.UnsafeString(contentBytes)
			for _, item := range entryOutputs {
				if strings.Index(content, item.EntryPath) != -1 {
					content = strings.ReplaceAll(content, item.EntryPath, item.OutputPath)
				}
			}
			err = os.WriteFile(path.Join(options.Outdir, htmlFile), generic.UnsafeStringBytes(content), 0644)
			if err != nil {
				message := fmt.Sprintf("Error writing entry html file %s: %s",
					htmlFile,
					err)
				generic.Append(&report.Messages, message)
			}
		}
	}

	htmlTime := time.Since(t2).Round(time.Millisecond)
	_ = htmlTime

	t3 := time.Now()

	// copy some files as-is
	for _, item := range options.CopyItems {
		err := copy.Copy(path.Join(options.FERoot, item), path.Join(options.Outdir, item))
		if err != nil {
			message := fmt.Sprintf("Error copying item %s: %s",
				item,
				err)
			generic.Append(&report.Messages, message)
		}
	}
	copyTime := time.Since(t3).Round(time.Millisecond)
	_ = copyTime

	/*
		log.Println("Frontend Build Time:", time.Since(t0).Round(time.Millisecond))
		if false {
			log.Println("                clean:", cleanTime)
			log.Println("                build:", buildTime)
			log.Println("                html:", htmlTime)
			log.Println("                copy:", copyTime)
		}
	*/

	report.Time = time.Now()
	report.Duration = report.Time.Sub(t0)
	report.Done = true
	ch <- report

	return true
}

func FEWatch(options FEBuildOptions, watchDirs []string, ch chan ESReport) {
	callback := func() {
		// log.Println("Building frontend")
		FEBuild(options, ch)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("Watching is not supported:", err)
		return
	}

	for _, dir := range watchDirs {
		err = watcher.Add(dir)
		if err != nil {
			fmt.Println("WARNING: Error watching directory", dir)
			fmt.Println("\t", err)
			continue
		}
	}

	// build once at first
	callback()

	var next time.Time
	var waitTime = 100 * time.Millisecond
	for event := range watcher.Events {
		if !(event.Op == fsnotify.Write || event.Op == fsnotify.Create) {
			// we don't care about these other events
			continue
		}

		var now = time.Now()
		// TODO ignore for now and see
		if event.Op == fsnotify.Create {
			watcher.Add(event.Name)
		}

		if now.Before(next) {
			// fmt.Println("skipping file event handler; next is scheduled at", next)
			continue
		}
		// log.Println("change:", event.Name)
		next = now.Add(waitTime)
		time.AfterFunc(waitTime, callback)
	}
}
