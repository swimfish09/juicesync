// Copyright (C) 2018-present Juicedata Inc.

package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"

	sapi "cloud.google.com/go/storage"
	storage "google.golang.org/api/storage/v1"
)

var ctx = context.Background()

type gs struct {
	defaultObjectStorage
	service   *storage.ObjectsService
	bucket    string
	region    string
	pageToken string
}

func (g *gs) String() string {
	return fmt.Sprintf("gs://%s", g.bucket)
}

func (g *gs) Create() error {
	// check if the bucket is already exists
	if _, err := g.List("", "", 1); err == nil {
		return nil
	}

	ctx := context.Background()
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		cred, err := google.FindDefaultCredentials(ctx)
		if err == nil {
			projectID = cred.ProjectID
		}
	}
	if projectID == "" {
		return errors.New("GOOGLE_CLOUD_PROJECT environment variable must be set")
	}

	client, err := sapi.NewClient(ctx)
	if err != nil {
		return err
	}

	bucket := client.Bucket(g.bucket)
	attr := &sapi.BucketAttrs{StorageClass: "regional", Location: g.region}
	err = bucket.Create(ctx, projectID, attr)
	if err != nil && strings.Contains(err.Error(), "You already own this bucket") {
		err = nil
	}
	return err
}

func (g *gs) Get(key string, off, limit int64) (io.ReadCloser, error) {
	req := g.service.Get(g.bucket, key)
	header := req.Header()
	if off > 0 || limit > 0 {
		if limit > 0 {
			header.Add("Range", fmt.Sprintf("bytes=%d-%d", off, off+limit-1))
		} else {
			header.Add("Range", fmt.Sprintf("bytes=%d-", off))
		}
	}
	resp, err := req.Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (g *gs) Put(key string, data io.Reader) error {
	obj := &storage.Object{Name: key}
	_, err := g.service.Insert(g.bucket, obj).Media(data).Do()
	return err
}

func (g *gs) Copy(dst, src string) error {
	_, err := g.service.Copy(g.bucket, src, g.bucket, dst, nil).Do()
	return err
}

func (g *gs) Exists(key string) error {
	_, err := g.Get(key, 0, 1)
	return err
}

func (g *gs) Delete(key string) error {
	if err := g.Exists(key); err != nil {
		return err
	}
	return g.service.Delete(g.bucket, key).Do()
}

func (g *gs) List(prefix, marker string, limit int64) ([]*Object, error) {
	call := g.service.List(g.bucket).Prefix(prefix).MaxResults(limit)
	if marker != "" {
		if g.pageToken == "" {
			// last page
			return nil, nil
		}
		call.PageToken(g.pageToken)
	}
	objects, err := call.Do()
	if err != nil {
		g.pageToken = ""
		return nil, err
	}
	g.pageToken = objects.NextPageToken
	n := len(objects.Items)
	objs := make([]*Object, n)
	for i := 0; i < n; i++ {
		item := objects.Items[i]
		ctime, _ := time.Parse(time.RFC3339, item.TimeCreated)
		mtime, _ := time.Parse(time.RFC3339, item.Updated)
		objs[i] = &Object{item.Name, int64(item.Size), int(ctime.Unix()), int(mtime.Unix())}
	}
	return objs, nil
}

func newGS(endpoint, accessKey, secretKey string) ObjectStorage {
	uri, err := url.ParseRequestURI(endpoint)
	if err != nil {
		logger.Fatalf("Invalid endpoint: %v, error: %v", endpoint, err)
	}
	hostParts := strings.Split(uri.Host, ".")
	bucket := hostParts[0]
	region := hostParts[1]
	client, err := google.DefaultClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	service, err := storage.New(client)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}
	return &gs{service: service.Objects, bucket: bucket, region: region}
}

func init() {
	register("gs", newGS)
}
