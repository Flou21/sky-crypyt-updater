package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/rs/zerolog/log"
	"io/ioutil"
	"os"
	"os/exec"
	"time"
)

const (
	localSkycryptPath = "skycrypt"
	tarFile           = "skycrypt.tar.gz"
)

func main() {

	clone()
	defer deleteFork()

	patches()

	patchCredentials()

	pack()
	defer deleteArchive()

	pushArtifact()

	updateMetrics()
}

func clone() {
	log.Info().Msgf("cloning skycrypt fork")

	_, err := git.PlainClone(localSkycryptPath, false, &git.CloneOptions{
		URL:           gitUrl(),
		Progress:      os.Stdout,
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", "development")),
		SingleBranch:  true,
	})

	if err != nil {
		log.Panic().Err(err).Msgf("error cloning")
	}

	log.Info().Msgf("cloned skycrypt fork")
}

func deleteFork() {
	err := os.RemoveAll(localSkycryptPath)
	if err != nil {
		log.Panic().Err(err).Msgf("error deleting local fork")
	}
}

func patches() {
	patchLinkToAhHistory()
	patchDockerfile()
}

func patchLinkToAhHistory() {

	patchFile := "./patches/add_link_to_ah_history.patch"
	toPatchFile := fmt.Sprintf("%s%s", localSkycryptPath, "/views/stats.ejs")

	patch(patchFile, toPatchFile)
}

func patchDockerfile() {
	patchFile := "./patches/dockerfile.patch"
	toPatchFile := fmt.Sprintf("%s%s", localSkycryptPath, "/Dockerfile")

	patch(patchFile, toPatchFile)
}

// patch changes the coflnet specific patches
func patch(patchFile, toPatchFile string) {
	patch, err := os.Open(patchFile)

	if err != nil {
		log.Panic().Err(err).Msgf("error opening patch file: %s", patchFile)
	}

	defer func(patch *os.File) {
		err := patch.Close()
		if err != nil {
			log.Error().Err(err).Msgf("could not gracefully close patch file: %s", patchFile)
		}
	}(patch)

	log.Debug().Msgf("applying patching: %s", patchFile)

	files, _, err := gitdiff.Parse(patch)
	if err != nil {
		log.Panic().Err(err).Msgf("error parsing patch file: %s", patchFile)
	}

	toPatch, err := os.OpenFile(toPatchFile, os.O_CREATE|os.O_APPEND, os.ModePerm)
	if err != nil {
		log.Panic().Err(err).Msgf("error opening patch file: %s", toPatchFile)
	}

	log.Debug().Msgf("applying patching: %s", toPatchFile)

	var output bytes.Buffer
	err = gitdiff.Apply(&output, toPatch, files[0])

	if err != nil {
		log.Panic().Err(err).Msgf("error patching")
	}

	err = toPatch.Close()
	if err != nil {
		log.Error().Err(err).Msgf("could not gracefully close patch file: %s", toPatchFile)
	}

	err = ioutil.WriteFile(toPatchFile, output.Bytes(), os.ModePerm)
	if err != nil {
		log.Panic().Err(err).Msgf("error writing patch file: %s", toPatchFile)
	}

	log.Info().Msgf("patched %s with %s", toPatchFile, patchFile)
}

// patchCredentials configures the credentials for the git repository
func patchCredentials() {
	patchFile := "./creds/credentials.patch"
	toPatchFile := fmt.Sprintf("%s%s", localSkycryptPath, "/src/credentials.js")

	patch, err := os.Open(patchFile)
	if err != nil {
		log.Panic().Err(err).Msgf("error opening patch file: %s", patchFile)
	}

	defer func(patch *os.File) {
		err := patch.Close()
		if err != nil {
			log.Error().Err(err).Msgf("could not gracefully close patch file: %s", patchFile)
		}
	}(patch)

	log.Debug().Msgf("applying patching: %s", patchFile)

	files, _, err := gitdiff.Parse(patch)
	if err != nil {
		log.Panic().Err(err).Msgf("error parsing patch file: %s", patchFile)
	}

	toPatch, err := os.OpenFile(toPatchFile, os.O_CREATE|os.O_APPEND, os.ModePerm)
	if err != nil {
		log.Panic().Err(err).Msgf("error opening patch file: %s", toPatchFile)
	}

	log.Debug().Msgf("applying patching: %s", toPatchFile)

	var output bytes.Buffer
	err = gitdiff.Apply(&output, toPatch, files[0])

	if err != nil {
		log.Panic().Err(err).Msgf("error patching")
	}

	err = toPatch.Close()
	if err != nil {
		log.Error().Err(err).Msgf("could not gracefully close patch file: %s", toPatchFile)
	}

	err = ioutil.WriteFile(toPatchFile, output.Bytes(), os.ModePerm)
	if err != nil {
		log.Panic().Err(err).Msgf("error writing patch file: %s", toPatchFile)
	}

	log.Info().Msgf("patched %s with %s", toPatchFile, patchFile)
}

// create a .tar.gz file from the skycrypt folder
func pack() {

	output, err := exec.Command("tar", "-czf", tarFile, localSkycryptPath).CombinedOutput()

	if err != nil {
		log.Panic().Err(err).Msgf("error creating tar file: %s", tarFile)
	}
	log.Info().Msgf("created tar file\n%s", output)

	log.Info().Msgf("packed skycrypt")
}

// push create a .tar.gz file and pushes it to minio
func pushArtifact() {
	client := minioClient()
	bucket := os.Getenv("MINIO_BUCKET")

	if bucket == "" {
		log.Panic().Msgf("MINIO_BUCKET is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b, err := client.BucketExists(ctx, bucket)
	if err != nil {
		log.Panic().Err(err).Msgf("error checking if bucket exists: %s", bucket)
	}

	if !b {
		log.Panic().Msgf("bucket does not exist: %s", bucket)
	}

	contentType := "application/x-gzip"

	_, err = client.FPutObject(ctx, bucket, tarFile, tarFile, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Panic().Err(err).Msgf("error uploading tar file: %s", tarFile)
	}

	log.Info().Msg("uploaded tar file")
}

func minioClient() *minio.Client {
	endpoint := os.Getenv("MINIO_HOST")
	accessKeyID := os.Getenv("MINIO_ACCESS_ID")
	secretAccessKey := os.Getenv("MINIO_ACCESS_KEY")

	if endpoint == "" || accessKeyID == "" || secretAccessKey == "" {
		log.Panic().Msgf("missing minio credentials")
	}

	// Initialize minio client object.
	client, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
	})

	if err != nil {
		log.Panic().Err(err).Msgf("error creating minio client")
	}

	return client
}

func gitUrl() string {
	g := os.Getenv("SKYCRYPT_FORK")

	if g == "" {
		log.Panic().Msgf("SKYCRYPT_FORK is not set")
	}

	return g
}

func deleteArchive() {
	err := os.Remove(tarFile)
	if err != nil {
		log.Error().Err(err).Msgf("error deleting tar file: %s", tarFile)
	}
}

func updateMetrics() {
	completionTime := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "db_backup_last_completion_timestamp_seconds",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	completionTime.SetToCurrentTime()

	err := push.New(prometheusHost(), "sky_crypt_updater_patched").
		Collector(completionTime).
		Push()

	if err != nil {
		log.Panic().Err(err).Msgf("error pushing prometheus metrics")
	}
}

func prometheusHost() string {
	s := os.Getenv("PROMETHEUS_HOST")
	if s == "" {
		log.Panic().Msgf("PROMETHEUS_HOST is not set")
		return ""
	}
	return s
}
