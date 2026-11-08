package store

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// fakeDynamo speaks just enough of the DynamoDB JSON protocol for the store.
type fakeDynamo struct {
	mu    sync.Mutex
	puts  []map[string]any
	scans []map[string]any
}

func (f *fakeDynamo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")

	f.mu.Lock()
	defer f.mu.Unlock()
	switch target := r.Header.Get("X-Amz-Target"); target {
	case "DynamoDB_20120810.PutItem":
		f.puts = append(f.puts, req)
		io.WriteString(w, `{}`)
	case "DynamoDB_20120810.Scan":
		f.scans = append(f.scans, req)
		if _, ok := req["ExclusiveStartKey"]; !ok {
			io.WriteString(w, `{"Items":[
				{"discordID":{"N":"123456789012345678"},"lastFMName":{"S":"alice"}},
				{"discordID":{"N":"2"},"lastFMName":{"S":"  "}}
			],"LastEvaluatedKey":{"discordID":{"N":"2"}}}`)
			return
		}
		io.WriteString(w, `{"Items":[{"discordID":{"N":"3"},"lastFMName":{"S":" bob "}}]}`)
	default:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"__type":"UnknownOperationException","message":"`+target+`"}`)
	}
}

func newTestStore(t *testing.T) (*Dynamo, *fakeDynamo) {
	t.Helper()
	fake := &fakeDynamo{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	cfg := aws.Config{
		Region:      "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
	}
	return newDynamo(cfg, "links", func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(srv.URL) }), fake
}

func TestUsersReadsEveryPage(t *testing.T) {
	d, fake := newTestStore(t)
	users, err := d.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []User{{DiscordID: 123456789012345678, LastFMName: "alice"}, {DiscordID: 3, LastFMName: "bob"}}
	if len(users) != len(want) || users[0] != want[0] || users[1] != want[1] {
		t.Fatalf("got %+v, want %+v (blank names skipped, whitespace trimmed)", users, want)
	}
	if len(fake.scans) != 2 {
		t.Fatalf("expected 2 scan pages, got %d", len(fake.scans))
	}
}

func TestLinkUserUsesExistingSchema(t *testing.T) {
	d, fake := newTestStore(t)
	if err := d.LinkUser(context.Background(), User{DiscordID: 123456789012345678, LastFMName: "alice"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.puts) != 1 {
		t.Fatalf("got %d puts", len(fake.puts))
	}
	b, _ := json.Marshal(fake.puts[0])
	got := string(b)
	for _, want := range []string{`"TableName":"links"`, `"discordID":{"N":"123456789012345678"}`, `"lastFMName":{"S":"alice"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("PutItem request %s is missing %s", got, want)
		}
	}
}

func TestPing(t *testing.T) {
	d, fake := newTestStore(t)
	if err := d.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.scans) != 1 || fake.scans[0]["Limit"] != float64(1) {
		t.Fatalf("ping should be a single 1-item scan, got %+v", fake.scans)
	}
}
