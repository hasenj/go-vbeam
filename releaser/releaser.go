package releaser

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/fatih/color"

	"go.hasen.dev/generic"
	"go.hasen.dev/vbeam/esbuilder"
)

func ReleaseOutputFileName(releasesDir string, name string, now time.Time) string {
	var version = name + "-" + generic.TimeStamp3Format(now, 5)
	var count = 0
	var names = make(map[string]bool)
	var items, _ = os.ReadDir(releasesDir)
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if strings.HasPrefix(item.Name(), version) {
			count += 1
			names[item.Name()] = true
		}
	}

	if count > 0 {
		var suffix = count
		var newVersion = version
		for {
			newVersion = fmt.Sprintf("%s-%03d", version, suffix)
			exists, _ := names[newVersion]
			if !exists {
				break
			}
			suffix += 1
		}
		version = newVersion
	}
	return version
}

type GoReleaseOptions struct {
	Name       string
	CmdPackage string
	CGO        bool
}

type ReleaseOptions struct {
	GoOnly bool
	GoReleaseOptions
	ESBOptions esbuilder.FEBuildOptions
	OutputDir  string
}

const DEFAULT_OUTPUT_DIR = "releases"

// TODO data for progress report and function to display it
func Release(opts ReleaseOptions) bool {
	var startTime = time.Now()
	if opts.OutputDir == "" {
		opts.OutputDir = DEFAULT_OUTPUT_DIR
	}
	os.MkdirAll(opts.OutputDir, 0755)

	h := color.New(color.Bold)
	version := ReleaseOutputFileName(opts.OutputDir, opts.Name, startTime)
	fmt.Println("Releasing", version)

	var stage = 0

	var tsStartTime time.Time
	var tscmd *exec.Cmd // the typescript command
	var tsout bytes.Buffer
	if !opts.GoOnly {
		stage++
		h.Printf("\n%d) Typechecking Frontend in the background\n", stage)
		tscmd = exec.Command("npx", "tsc", "--noEmit", "--pretty", "false")
		tscmd.Stdout = &tsout
		tscmd.Stderr = &tsout
		var err = tscmd.Start()
		if err != nil {
			fmt.Println("Could not start the typechecker")
			fmt.Println(err)
			fmt.Println("Exiting.")
			return false
		}
		tsStartTime = time.Now()
	}

	if !opts.GoOnly {
		stage++
		h.Printf("\n%d) Building Frontend\n", stage)
		var options = opts.ESBOptions
		fmt.Printf("Frontend Output Directory: %s\n", options.Outdir)
		// delete the existing build directory, if any
		var err = os.RemoveAll(options.Outdir)
		if err != nil {
			fmt.Println("Could not clear previous frontend build directory. Try to delete it manually")
			fmt.Println(options.Outdir)
			return false
		}
		// TODO: pass a reporting channel?
		var ok = esbuilder.FEBuild(options, nil)
		if !ok {
			fmt.Println("\n\nFailed.")
			return false
		}
	}

	{
		stage++
		h.Printf("\n%d) Building Executable\n", stage)
		// GOOS=linux CGO_ENABLED=1 CC=x86_64-linux-musl-gcc go build -ldflags "-w -s -linkmode external -extldflags -static" -o "bin/$name" ./cmd/prod_app
		var cmd = exec.Command("go", "build",
			"-ldflags", "-s -w",
			// "-ldflags", "-w -s -linkmode external -extldflags -static", // this is for linking with C
			"-tags", "release",
			"-o", path.Join(opts.OutputDir, version),
			opts.CmdPackage,
		)
		if cmd.Err != nil {
			fmt.Println("Go compiler not found!", cmd.Err)
			return false
		}

		cmd.Env = append(cmd.Environ(),
			"GOOS=linux", "GOARCH=amd64",
		)
		if opts.CGO {
			cmd.Env = append(cmd.Env,
				"CGO_ENABLED=1",
				"CC=zig cc -target x86_64-linux -Wno-implicit-const-int-float-conversion",
			)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		var start = time.Now()
		var err = cmd.Run()
		if err != nil {
			fmt.Println("\n\nFailed!")
			return false
		}
		fmt.Printf("%s\n", time.Since(start))
	}

	// check the status of the frontend type checking
	if !opts.GoOnly {
		stage++
		h.Printf("\n%d) Waiting for Frontend Typechecking to finish\n", stage)
		var err = tscmd.Wait()
		if err != nil {
			fmt.Println("Frontend Typechecking failed")
			fmt.Println(tsout.String())
			return false
		}
		fmt.Println("Typecheck passed", time.Since(tsStartTime))
	}

	outputPath := path.Join(opts.OutputDir, version)

	// print the file size
	{
		fileInfo, _ := os.Stat(outputPath)
		fileSize := fileInfo.Size()
		fmt.Printf("Output file size: %.2fMB", float64(fileSize)/(1024*1024))
	}

	fmt.Println("\nTotal time:", time.Since(startTime))
	fmt.Println()
	fmt.Println(outputPath)
	return true
}
