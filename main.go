package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sync"

	"github.com/gorilla/mux"
	flags "github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{
	"service": "vice-file-transfers",
	"art-id":  "vice-file-transfers",
	"group":   "org.cyverse",
})

var (
	uploadRunning        bool
	uploadRunningMutex   sync.Mutex
	downloadRunning      bool
	downloadRunningMutex sync.Mutex
)

// App contains application state.
type App struct {
	LogDirectory        string
	User                string
	UploadDestination   string
	DownloadDestination string
	InvocationID        string
	InputPathList       string
	ExcludesPath        string
	ConfigPath          string
	FileMetadata        []string
}

func (a *App) downloadCommand() []string {
	retval := []string{
		"porklock",
		"-jar",
		"/usr/src/app/porklock-standalone.jar",
		"get",
		"--user", a.User,
		"--source-list", a.InputPathList,
		"--destination", a.DownloadDestination,
		"-z", a.ConfigPath,
	}
	for _, fm := range a.FileMetadata {
		retval = append(retval, "-m", fm)
	}
	return retval
}

func (a *App) fileUseable(aPath string) bool {
	if _, err := os.Stat(aPath); err != nil {
		return false
	}
	return true
}

// DownloadFiles handles requests to download files.
func (a *App) DownloadFiles(writer http.ResponseWriter, req *http.Request) {
	log.Info("received download request")

	downloadRunningMutex.Lock()
	shouldRun := !downloadRunning && a.fileUseable(a.InputPathList)
	downloadRunningMutex.Unlock()

	if shouldRun {
		log.Info("starting download goroutine")

		go func() {
			log.Info("running download goroutine")

			var (
				downloadLogStderrFile *os.File
				downloadLogStdoutFile *os.File
				downloadLogStderrPath string
				downloadLogStdoutPath string
				err                   error
			)

			downloadRunningMutex.Lock()
			downloadRunning = true
			downloadRunningMutex.Unlock()

			defer func() {
				downloadRunningMutex.Lock()
				downloadRunning = false
				downloadRunningMutex.Unlock()
			}()

			downloadLogStdoutPath = path.Join(a.LogDirectory, "downloads.stdout.log")
			downloadLogStdoutFile, err = os.Create(downloadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStdoutPath))
				return

			}

			downloadLogStderrPath = path.Join(a.LogDirectory, "downloads.stderr.log")
			downloadLogStderrFile, err = os.Create(downloadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStderrPath))
				return
			}

			parts := a.downloadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = downloadLogStdoutFile
			cmd.Stderr = downloadLogStderrFile
			if err = cmd.Run(); err != nil {
				log.Error(errors.Wrap(err, "error running porklock for downloads"))
				return
			}

			log.Info("exiting download goroutine without errors")
		}()
	} else {
		log.Info("not running a download gorouting for this request")
	}
}

func (a *App) uploadCommand() []string {
	retval := []string{
		"porklock",
		"-jar",
		"/usr/src/app/porklock-standalone.jar",
		"put",
		"--user", a.User,
		"--source", a.DownloadDestination,
		"--destination", a.UploadDestination,
		"--exclude", a.ExcludesPath,
		"-z", a.ConfigPath,
	}
	for _, fm := range a.FileMetadata {
		retval = append(retval, "-m", fm)
	}
	return retval
}

// UploadFiles handles requests to upload files.
func (a *App) UploadFiles(writer http.ResponseWriter, req *http.Request) {
	log.Info("received upload request")

	uploadRunningMutex.Lock()
	shouldRun := !uploadRunning
	uploadRunning = true
	uploadRunningMutex.Unlock()

	if shouldRun {
		log.Info("starting upload goroutine")

		go func() {
			log.Info("running upload goroutine")

			uploadLogStdoutPath := path.Join(a.LogDirectory, "uploads.stdout.log")
			uploadLogStdoutFile, err := os.Create(uploadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStdoutPath))
				return
			}

			uploadLogStderrPath := path.Join(a.LogDirectory, "uploads.stderr.log")
			uploadLogStderrFile, err := os.Create(uploadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStderrPath))
				return
			}

			parts := a.uploadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = uploadLogStdoutFile
			cmd.Stderr = uploadLogStderrFile
			if err = cmd.Run(); err != nil {
				log.Error(errors.Wrap(err, "error running porklock for uploads"))
				return
			}

			uploadRunningMutex.Lock()
			uploadRunning = false
			uploadRunningMutex.Unlock()

			log.Info("exiting upload goroutine without errors")
		}()
	}
}

func main() {
	var options struct {
		ListenPort          int      `short:"l" long:"listen-port" default:"60001" description:"The port to listen on for requests"`
		LogDirectory        string   `long:"log-dir" default:"/input-files" description:"The directory in which to write log files"`
		User                string   `long:"user" required:"true" description:"The user to run the transfers for"`
		UploadDestination   string   `long:"upload-destination" required:"true" description:"The destination directory for uploads"`
		DownloadDestination string   `long:"download-destination" default:"/input-files" description:"The destination directory for downloads"`
		ExcludesFile        string   `long:"excludes-file" default:"/excludes/excludes-file" description:"The path to the excludes file"`
		PathListFile        string   `long:"path-list-file" default:"/input-paths/input-path-list" description:"The path to the input paths list file"`
		IRODSConfig         string   `long:"irods-config" default:"/etc/porklock/irods-config.properties" description:"The path to the porklock iRODS config file"`
		InvocationID        string   `long:"invocation-id" required:"true" description:"The invocation UUID"`
		FileMetadata        []string `short:"m" description:"Metadata to apply to files"`
	}

	if _, err := flags.Parse(&options); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		log.Fatal(err)
	}

	_, err := exec.LookPath("porklock")
	if err != nil {
		log.Fatal(err)
	}

	app := &App{
		LogDirectory:        options.LogDirectory,
		InvocationID:        options.InvocationID,
		ConfigPath:          options.IRODSConfig,
		User:                options.User,
		UploadDestination:   options.UploadDestination,
		DownloadDestination: options.DownloadDestination,
		ExcludesPath:        options.ExcludesFile,
		InputPathList:       options.PathListFile,
		FileMetadata:        options.FileMetadata,
	}

	router := mux.NewRouter()
	router.HandleFunc("/download", app.DownloadFiles).Methods(http.MethodPost)
	router.HandleFunc("/upload", app.UploadFiles).Methods(http.MethodPost)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", options.ListenPort), router))

}
