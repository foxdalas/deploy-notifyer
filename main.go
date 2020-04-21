package main

import (
	"deploy-notifyer/pkg/elastic"
	"encoding/json"
	"fmt"
	"github.com/parnurzeal/gorequest"
	"log"
	"os"
	"strings"
	"time"
)

const timeHuman = "02 Jan 15:04 MST"

func main() {
	layoutISO := "2006-01-02"
	yesterday := time.Now().AddDate(0, 0, -1)

	client, err := elastic.New(strings.Split(os.Getenv("ELASTICSEARCH"), ","), os.Getenv("INDEX"), os.Getenv("KIBANA_INDEX"), yesterday)
	if err != nil {
		log.Fatal(err)
	}

	data, err := client.GetDeploys(client.Ctx, client.Client)
	if err != nil {
		log.Print(err)
	}

	head := fmt.Sprintf("Вчера *%s* было деплоев\n*%d* всего\n", time.Now().AddDate(0, 0, -1).Format(layoutISO), data.Total)
	head += fmt.Sprintf("*%d* production\n", data.Production.Total)
	head += fmt.Sprintf("*%d* development\n\n", data.Development.Total)

	if len(data.Production.Users) > 0 {
		head += fmt.Sprint("*Топ 3 пользователей деплоевших в production:*\n")
		for _, user := range data.Production.Users {
			head += fmt.Sprintf("%s *%d*\n", user.Name, user.Count)
		}
	}
	if len(data.Development.Users) > 0 {
		head += fmt.Sprint("\n*Топ 3 пользователей деплоевших в development:*\n")
		for _, user := range data.Development.Users {
			head += fmt.Sprintf("%s *%d*\n", user.Name, user.Count)
		}
	}
	if len(data.Production.AfterWork) > 0 {
		loc, _ := time.LoadLocation("Asia/Bangkok")
		head += fmt.Sprint("\n*Деплои после 21:00 в production:*\n")
		for _, deploy := range data.Production.AfterWork {
			head += fmt.Sprintf("%s %s в дата-центр %s\n", deploy.Timestamp.In(loc).Format(timeHuman), deploy.User, deploy.Datacenter)
		}
	}
	payload := make(map[string]interface{})
	payload["channel"] = os.Getenv("CHANNEL")
	payload["text"] = head
	payload["username"] = "Максим"
	payload["mrkdwn"] = true

	d, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("error on encode request, %v", err)
	}

	_, _, errors := gorequest.New().Post(os.Getenv("SLACK")).Send(string(d)).End()
	if len(errors) > 0 {
		log.Fatalf("error on send request, %#v", errors)
	}
}
