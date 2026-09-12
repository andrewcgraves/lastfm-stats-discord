package framework

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/kurin/blazer/b2"
)

var bucket *b2.Bucket

func InitBackblaze(accountId string, applicationKey string, bucketName string) {
	ctx := context.Background()
	client, err := b2.NewClient(ctx, accountId, applicationKey)
	if err != nil {
		log.Printf("failed to connect to Backblaze: %s", err)
		return
	}

	bucket, err = client.Bucket(ctx, bucketName)
	if err != nil {
		log.Printf("failed to open Backblaze bucket %q: %s", bucketName, err)
		return
	}
	fmt.Println("Connected to Backblaze...")
}

func UploadFile(src, dest string) (string, error) {
	if bucket == nil {
		return "", errors.New("backblaze bucket is not initialized")
	}

	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()

	obj := bucket.Object(dest)
	w := obj.NewWriter(context.Background())
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return "", err
	}

	obj.URL()

	return obj.URL(), w.Close()
}
