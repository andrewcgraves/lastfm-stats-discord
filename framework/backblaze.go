package framework

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/kurin/blazer/b2"
)

var bucket *b2.Bucket

func InitBackblaze(accountId string, applicationKey string, bucketName string) {
	ctx := context.Background()
	b2, _ := b2.NewClient(ctx, accountId, applicationKey)
	bucket, _ = b2.Bucket(ctx, bucketName)
	fmt.Println("Connected to Backblaze...")
}

func UploadFile(src, dest string) (string, error) {
	f, _ := os.Open(src)

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
