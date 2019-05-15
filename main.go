package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	flags "github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const nonBlockingKey = "non-blocking"

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

const (
	// UploadKind represents an upload record
	UploadKind = "upload"

	// DownloadKind represents an download record
	DownloadKind = "download"

	// RequestedStatus means the the transfer has been requested but hasn't started
	RequestedStatus = "requested"

	// DownloadingStatus means that a downloading request is running
	DownloadingStatus = "downloading"

	// UploadingStatus means that an uploading request is running
	UploadingStatus = "uploading"

	// FailedStatus means that the transfer request failed
	FailedStatus = "failed"

	//CompletedStatus means that the transfer request succeeded
	CompletedStatus = "completed"
)

// TransferRecord records info about uploads and downloads.
type TransferRecord struct {
	UUID           uuid.UUID `json:"uuid"`
	StartTime      time.Time `json:"start_time"`
	CompletionTime time.Time `json:"completion_time"`
	Status         string    `json:"status"`
	Kind           string    `json:"kind"`
	mutex          sync.Mutex
}

// NewDownloadRecord returns a TransferRecord filled out with a UUID,
// StartTime, Status of "requested", and a Kind of "download".
func NewDownloadRecord() *TransferRecord {
	return &TransferRecord{
		UUID:      uuid.New(),
		StartTime: time.Now(),
		Status:    RequestedStatus,
		Kind:      DownloadKind,
	}
}

// NewUploadRecord returns a TransferRecord filled out with a UUID,
// StartTime, Status of "requested", and a Kind of "upload".
func NewUploadRecord() *TransferRecord {
	return &TransferRecord{
		UUID:      uuid.New(),
		StartTime: time.Now(),
		Status:    RequestedStatus,
		Kind:      DownloadKind,
	}
}

// MarshalAndWrite serializes the TransferRecord to json and writes it out using writer.
func (r *TransferRecord) MarshalAndWrite(writer io.Writer) error {
	var (
		recordbytes []byte
		err         error
	)

	r.mutex.Lock()
	if recordbytes, err = json.Marshal(r); err != nil {
		r.mutex.Unlock()
		return errors.Wrap(err, "error serializing download record")
	}
	r.mutex.Unlock()

	_, err = writer.Write(recordbytes)
	return err
}

// SetCompletionTime sets the CompletionTime field for the TransferRecord to the current time.
func (r *TransferRecord) SetCompletionTime() {
	r.mutex.Lock()
	r.CompletionTime = time.Now()
	r.mutex.Unlock()
}

// SetStatus sets the Status field for the TransferRecord to the provided value.
func (r *TransferRecord) SetStatus(status string) {
	r.mutex.Lock()
	r.Status = status
	r.mutex.Unlock()
}

// HistoricalRecords maintains a list of []*TransferRecords and provides thread-safe access
// to them.
type HistoricalRecords struct {
	records []*TransferRecord
	mutex   sync.Mutex
}

// Append adds another *TransferRecord to the list.
func (h *HistoricalRecords) Append(tr *TransferRecord) {
	h.mutex.Lock()
	h.records = append(h.records, tr)
	h.mutex.Unlock()
}

// FindRecord looks up a record by UUID and returns the pointer to it. The lookup is locked
// to prevent dirty reads. Return value will be nil if no records are found with the provided
// id.
func (h *HistoricalRecords) FindRecord(id string) *TransferRecord {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, dr := range h.records {
		if dr.UUID.String() == id {
			return dr
		}
	}

	return nil
}

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
	downloadWait        sync.WaitGroup
	uploadWait          sync.WaitGroup
	uploadRecords       *HistoricalRecords
	downloadRecords     *HistoricalRecords
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

	downloadRecord := NewDownloadRecord()
	a.downloadRecords.Append(downloadRecord)

	downloadRunningMutex.Lock()
	shouldRun := !downloadRunning && a.fileUseable(a.InputPathList)
	downloadRunningMutex.Unlock()

	if shouldRun {
		log.Info("starting download goroutine")

		a.downloadWait.Add(1)

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

			downloadRecord.SetStatus(DownloadingStatus)

			defer func() {
				downloadRecord.SetCompletionTime()

				downloadRunningMutex.Lock()
				downloadRunning = false
				downloadRunningMutex.Unlock()

				a.downloadWait.Done()
			}()

			downloadLogStdoutPath = path.Join(a.LogDirectory, "downloads.stdout.log")
			downloadLogStdoutFile, err = os.Create(downloadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStdoutPath))
				downloadRecord.SetStatus(FailedStatus)
				return

			}

			downloadLogStderrPath = path.Join(a.LogDirectory, "downloads.stderr.log")
			downloadLogStderrFile, err = os.Create(downloadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", downloadLogStderrPath))
				downloadRecord.SetStatus(FailedStatus)
				return
			}

			parts := a.downloadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = downloadLogStdoutFile
			cmd.Stderr = downloadLogStderrFile

			if err = cmd.Run(); err != nil {
				log.Error(errors.Wrap(err, "error running porklock for downloads"))
				downloadRecord.SetStatus(FailedStatus)
				return
			}

			downloadRecord.SetStatus(CompletedStatus)

			log.Info("exiting download goroutine without errors")
		}()
	}

	if err := downloadRecord.MarshalAndWrite(writer); err != nil {
		log.Error(err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

// GetDownloadStatus returns the status of the possibly running download.
func (a *App) GetDownloadStatus(writer http.ResponseWriter, request *http.Request) {
	id := mux.Vars(request)["id"]

	foundRecord := a.downloadRecords.FindRecord(id)
	if foundRecord == nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	if err := foundRecord.MarshalAndWrite(writer); err != nil {
		log.Error(err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

// GetUploadStatus returns the status of the possibly running upload.
func (a *App) GetUploadStatus(writer http.ResponseWriter, request *http.Request) {
	id := mux.Vars(request)["id"]

	foundRecord := a.uploadRecords.FindRecord(id)
	if foundRecord == nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	if err := foundRecord.MarshalAndWrite(writer); err != nil {
		log.Error(err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
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

	uploadRecord := NewUploadRecord()
	a.uploadRecords.Append(uploadRecord)

	uploadRunningMutex.Lock()
	shouldRun := !uploadRunning
	uploadRunning = true
	uploadRunningMutex.Unlock()

	if shouldRun {
		log.Info("starting upload goroutine")

		a.uploadWait.Add(1)

		go func() {
			log.Info("running upload goroutine")

			defer func() {
				uploadRecord.SetCompletionTime()

				uploadRunningMutex.Lock()
				uploadRunning = false
				uploadRunningMutex.Unlock()

				a.uploadWait.Done()
			}()

			uploadLogStdoutPath := path.Join(a.LogDirectory, "uploads.stdout.log")
			uploadLogStdoutFile, err := os.Create(uploadLogStdoutPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStdoutPath))
				uploadRecord.SetStatus(FailedStatus)
				return
			}

			uploadLogStderrPath := path.Join(a.LogDirectory, "uploads.stderr.log")
			uploadLogStderrFile, err := os.Create(uploadLogStderrPath)
			if err != nil {
				log.Error(errors.Wrapf(err, "failed to open file %s", uploadLogStderrPath))
				uploadRecord.SetStatus(FailedStatus)
				return
			}

			parts := a.uploadCommand()
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdout = uploadLogStdoutFile
			cmd.Stderr = uploadLogStderrFile

			if err = cmd.Run(); err != nil {
				log.Error(errors.Wrap(err, "error running porklock for uploads"))
				uploadRecord.SetStatus(FailedStatus)
				return
			}

			uploadRecord.SetStatus(CompletedStatus)

			log.Info("exiting upload goroutine without errors")
		}()
	}

	if err := uploadRecord.MarshalAndWrite(writer); err != nil {
		log.Error(err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
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
		downloadWait:        sync.WaitGroup{},
		uploadWait:          sync.WaitGroup{},
		uploadRecords:       &HistoricalRecords{},
		downloadRecords:     &HistoricalRecords{},
	}

	router := mux.NewRouter()
	router.HandleFunc("/download", app.DownloadFiles).Queries(nonBlockingKey, "").Methods(http.MethodPost)
	router.HandleFunc("/download", app.DownloadFiles).Methods(http.MethodPost)
	router.HandleFunc("/download/{id}", app.GetDownloadStatus).Methods(http.MethodGet)

	router.HandleFunc("/upload", app.UploadFiles).Queries(nonBlockingKey, "").Methods(http.MethodPost)
	router.HandleFunc("/upload", app.UploadFiles).Methods(http.MethodPost)
	router.HandleFunc("/upload/status/{id}", app.GetUploadStatus).Methods(http.MethodGet)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", options.ListenPort), router))

}
