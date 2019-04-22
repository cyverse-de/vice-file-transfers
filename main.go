package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sync"

	"github.com/gorilla/mux"
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
	LogDirectory  string
	User          string
	Destination   string
	AnalysisID    string
	InvocationID  string
	InputPathList string
	ExcludesPath  string
	ConfigPath    string
}

func (a *App) downloadCommand() []string {
	return []string{
		"porklock",
		"-jar",
		"/usr/src/app/porklock-standalone.jar",
		"get",
		"--user", a.User,
		"--source-list", a.InputPathList,
		"-m", fmt.Sprintf("ipc-analysis-id,%s,UUID", a.AnalysisID),
		"-m", fmt.Sprintf("ipc-execution-id,%s,UUID", a.InvocationID),
		"-z", a.ConfigPath,
	}
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
	downloadRunning = true
	downloadRunningMutex.Unlock()

	if shouldRun {
		log.Info("starting download goroutine")

		go func() {
			log.Info("running download goroutine")

			downloadLogStdoutPath := path.Join(a.LogDirectory, "downloads.stdout.log")
			downloadLogStdoutFile, err := os.Open(downloadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStdoutPath))
				return
			}

			downloadLogStderrPath := path.Join(a.LogDirectory, "downloads.stderr.log")
			downloadLogStderrFile, err := os.Open(downloadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStderrPath))
				return
			}

			parts := a.downloadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = downloadLogStdoutFile
			cmd.Stderr = downloadLogStderrFile
			if err = cmd.Wait(); err != nil {
				log.Error(errors.Wrap(err, "error running porklock for downloads"))
				return
			}

			downloadRunningMutex.Lock()
			downloadRunning = false
			downloadRunningMutex.Unlock()

			log.Info("exiting download goroutine without errors")
		}()
	}
}

func (a *App) uploadCommand() []string {
	return []string{
		"porklock",
		"-jar",
		"/usr/src/app/porklock-standalone.jar",
		"put",
		"--user", a.User,
		"--destination", a.Destination,
		"-m", fmt.Sprintf("ipc-analysis-id,%s,UUID", a.AnalysisID),
		"-m", fmt.Sprintf("ipc-execution-id,%s,UUID", a.InvocationID),
		"--exclude", a.ExcludesPath,
		"-z", a.ConfigPath,
	}
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
			uploadLogStdoutFile, err := os.Open(uploadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStdoutPath))
				return
			}

			uploadLogStderrPath := path.Join(a.LogDirectory, "uploads.stderr.log")
			uploadLogStderrFile, err := os.Open(uploadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStderrPath))
				return
			}

			parts := a.uploadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = uploadLogStdoutFile
			cmd.Stderr = uploadLogStderrFile
			if err = cmd.Wait(); err != nil {
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
	var (
		listenPort   = flag.Int("listen-port", 60000, "The port to listen on for requests")
		logDirectory = flag.String("log-dir", "/input-files", "The directory in which to write log files")
		user         = flag.String("user", "", "The user to run the transfers for")
		destination  = flag.String("destination", "", "The destination directory for uploads")
		excludesFile = flag.String("excludes-file", "/excludes/excludes-file", "The path to the excludes file")
		pathListFile = flag.String("path-list-file", "/input-paths/input-path-list", "The path to the input paths list file")
		irodsConfig  = flag.String("irods-config", "/etc/porklock/irods-config.properties", "The path to the porklock iRODS connection configuration file")
		analysisID   = flag.String("analysis-id", "", "The UUID for the DE Analysis the transfers are a part of")
		invocationID = flag.String("invocation-id", "", "The invocation UUID")
	)

	flag.Parse()

	_, err := exec.LookPath("porklock")
	if err != nil {
		log.Fatal(err)
	}

	app := &App{
		LogDirectory:  *logDirectory,
		AnalysisID:    *analysisID,
		InvocationID:  *invocationID,
		ConfigPath:    *irodsConfig,
		User:          *user,
		Destination:   *destination,
		ExcludesPath:  *excludesFile,
		InputPathList: *pathListFile,
	}

	router := mux.NewRouter()
	router.HandleFunc("/download", app.DownloadFiles).Methods(http.MethodPost)
	router.HandleFunc("/upload", app.UploadFiles).Methods(http.MethodPost)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *listenPort), router))

}
