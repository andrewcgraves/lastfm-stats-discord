package framework

import (
	"log"
)

func Check(err error) {
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

// type LastFMEntry struct {
// 	DiscordID  int    `dynamodbav:"discordID"`
// 	LastFMName string `dynamodbav:"lastFMName"`
// }

// func putDocdbEntry(entry LastFMEntry) (*dynamodb.PutItemOutput, error) {
// 	r, err := attributevalue.MarshalMap(entry)
// 	check(err)

// 	res, err := dyn.PutItem(context.Background(), &dynamodb.PutItemInput{
// 		TableName: aws.String(os.Getenv("TABLE_NAME")),
// 		Item:      r,
// 	})

// 	return res, err
// }
