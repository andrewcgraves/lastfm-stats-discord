package framework

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

var dyn *dynamodb.Client

type LastFMEntry struct {
	DiscordID  int    `dynamodbav:"discordID"`
	LastFMName string `dynamodbav:"lastFMName"`
}

func InitDBConnection() {
	cfg, _ := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-west-2"))
	dyn = dynamodb.NewFromConfig(cfg)
	fmt.Println("Connected to DB...")
}

func SaveUserConfig(entry LastFMEntry) error {
	r, err := attributevalue.MarshalMap(entry)
	Check(err)

	_, err = dyn.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(os.Getenv("TABLE_NAME")),
		Item:      r,
	})

	return err
}
