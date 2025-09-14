package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	// "thumbnail" should match the HTML form input name
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	// _, err = io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing media type", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "cannot upload anything other than jpeg or png", nil)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error fetching video", err)
		return
	}

	// imageDataStr := base64.StdEncoding.EncodeToString(imageData)
	// thumbnailUrl := fmt.Sprintf("data:%s;base64,%s", mediaType, imageDataStr)
	// http://localhost:<port>/assets/<videoID>.<file_extension>

	// /assets/<videoID>.<file_extension>
	// filepath := fmt.Sprint()
	parts := strings.Split(mediaType, "/")
	// filepath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", videoIDString, parts[len(parts)-1]))
	var byteArray [32]byte
	_, _ = rand.Read(byteArray[:])

	filepath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", base64.RawURLEncoding.EncodeToString(byteArray[:]), parts[len(parts)-1]))
	fmt.Println("creating file: ", filepath)
	destinationFile, err := os.Create(filepath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating new file", err)
	}

	fmt.Println("copying file: ", filepath)
	_, err = io.Copy(destinationFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file", err)
	}

	fmt.Println("done copying file")
	thumbnailUrl := fmt.Sprintf("http://localhost:8091/assets/%s.%s", base64.RawURLEncoding.EncodeToString(byteArray[:]), parts[len(parts)-1])
	video.ThumbnailURL = &thumbnailUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video thumbnail url", err)
	}

	respondWithJSON(w, http.StatusOK, video)

	defer destinationFile.Close()
	defer file.Close()

}
