package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Could not find JWT", err)
		return
	}

	userId, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not validate JWT", err)
		return
	}

	const maxMemory = 10 << 30
	http.MaxBytesReader(w, r.Body, maxMemory)

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not get video metadata", err)
		return
	}
	if video.UserID != userId {
		respondWithError(w, http.StatusUnauthorized, "Video does not belong to user", err)
		return
	}

	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to determine Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Uploaded content must be of type 'video/mp4'", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy from stream to temp file", err)
		return
	}

	ar, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting aspect ratio", err)
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error seeking temp file", err)
		return
	}

	var byteArray [32]byte
	_, _ = rand.Read(byteArray[:])
	parts := strings.Split(mediaType, "/")
	key := fmt.Sprintf("%s.%s", base64.RawURLEncoding.EncodeToString(byteArray[:]), parts[len(parts)-1])

	switch ar {
	case "16:9":
		key = fmt.Sprint("landscape/", key)
	case "9:16":
		key = fmt.Sprint("portrait/", key)
	default:
		key = fmt.Sprint("other/", key)
	}

	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        tempFile,
		ContentType: &mediaType,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file to s3", err)
		return
	}

	// https://<bucket-name>.s3.<region>.amazonaws.com/<key>
	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video with new url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, nil)

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	defer file.Close()
}

func getVideoAspectRatio(filepath string) (aspectRatio string, err error) {
	type stream struct {
		Height float64
		Width  float64
	}
	type cmdOutput struct {
		Streams []stream
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filepath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
		return "", fmt.Errorf("error running command: %w", err)
	}

	var output cmdOutput
	err = json.Unmarshal(stdout.Bytes(), &output)
	if err != nil {
		fmt.Print(err.Error())
		return "", fmt.Errorf("error reading cmd results: %w", err)
	}

	fmt.Println(stdout.String())
	fmt.Println(output)
	fmt.Printf("width: %v\n", output.Streams[0].Width)
	fmt.Printf("height: %v\n", output.Streams[0].Height)
	fmt.Printf("here5: %v\n", math.Round(output.Streams[0].Width/output.Streams[0].Height))
	if math.Round(output.Streams[0].Width/output.Streams[0].Height) == math.Round(16.0/9.0) {
		return "16:9", nil
	}
	if math.Round(output.Streams[0].Width/output.Streams[0].Height) == math.Round(9.0/16.0) {
		return "9:16", nil
	}
	return "other", nil
}
