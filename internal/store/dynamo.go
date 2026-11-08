// Package store persists the Discord ⇄ Last.fm account links in DynamoDB.
package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// User links a Discord account to a Last.fm username. The attribute names
// match the existing table, so no data migration is needed.
type User struct {
	DiscordID  int64  `dynamodbav:"discordID"`
	LastFMName string `dynamodbav:"lastFMName"`
}

// Store reads and writes account links.
type Store interface {
	LinkUser(ctx context.Context, u User) error
	Users(ctx context.Context) ([]User, error)
}

// Dynamo is a DynamoDB-backed Store.
type Dynamo struct {
	client *dynamodb.Client
	table  string
}

// NewDynamo loads AWS credentials the standard way (environment, shared config
// files, instance roles) and returns a store for table.
func NewDynamo(ctx context.Context, region, table string) (*Dynamo, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithRetryMaxAttempts(5),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return newDynamo(cfg, table), nil
}

func newDynamo(cfg aws.Config, table string, opts ...func(*dynamodb.Options)) *Dynamo {
	return &Dynamo{client: dynamodb.NewFromConfig(cfg, opts...), table: table}
}

// Ping checks that the table is reachable with the configured credentials.
// It uses a one-item Scan so it needs no IAM permission beyond what the digest
// already uses.
func (d *Dynamo) Ping(ctx context.Context) error {
	_, err := d.client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(d.table), Limit: aws.Int32(1)})
	return err
}

// LinkUser creates or replaces the link for u.DiscordID.
func (d *Dynamo) LinkUser(ctx context.Context, u User) error {
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		return fmt.Errorf("encoding user: %w", err)
	}
	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.table),
		Item:      item,
	})
	return err
}

// Users returns every linked account, reading all scan pages.
func (d *Dynamo) Users(ctx context.Context) ([]User, error) {
	var users []User
	p := dynamodb.NewScanPaginator(d.client, &dynamodb.ScanInput{TableName: aws.String(d.table)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", d.table, err)
		}
		for _, item := range page.Items {
			var u User
			if err := attributevalue.UnmarshalMap(item, &u); err != nil {
				return nil, fmt.Errorf("decoding user: %w", err)
			}
			u.LastFMName = strings.TrimSpace(u.LastFMName)
			if u.LastFMName == "" {
				continue
			}
			users = append(users, u)
		}
	}
	return users, nil
}
