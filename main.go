package main

import (
	"bytes"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"mqtt_consumer/config"
	"mqtt_consumer/mqttUtils"
	"mqtt_consumer/parser"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var cfg config.Config
var writeUrl string

func main() {
	config.LoadConfig(&cfg)

	persistData := len(cfg.InfluxDB.Url) > 0 && len(cfg.InfluxDB.Database) > 0

	if persistData {
		writeUrl = fmt.Sprintf(
			"%s/write?db=%s",
			cfg.InfluxDB.Url,
			cfg.InfluxDB.Database,
		)
	}

	jsonParser := parser.NewParser(cfg.Parser)

	mqttUtils.ConnectToMQTT(cfg.MqttBroker, getSubscriptionMethod(jsonParser, persistData))

	exitSignal := make(chan os.Signal)
	signal.Notify(exitSignal, syscall.SIGINT, syscall.SIGTERM)
	<-exitSignal
}

func getSubscriptionMethod(parser *parser.Parser, persistData bool) func(c mqtt.Client) {
	return func(client mqtt.Client) {
		client.Subscribe(cfg.MqttBroker.Topic, 0, func(client mqtt.Client, msg mqtt.Message) {
			payload := string(msg.Payload())
			json, err := parser.StringToJson(payload)
			if err != nil {
				log.Printf("Failed to convert %v into a JSON object", payload)
			}
			lineProtocol, err := parser.JsonToInfluxLineProtocol(json)
			if err != nil {
				log.Println(err.Error())
				return
			}

			if !persistData {
				log.Printf("Received data: %s", lineProtocol)
				return
			}
			statusCode, status, err := postDataToInflux(lineProtocol)
			if err != nil {
				log.Printf("Failed to send data %s to the database: %s", lineProtocol, err.Error())
			} else {
				switch statusCode {
				case 204:
					log.Printf("Wrote %s to InfluxDB", lineProtocol)
				default:
					log.Printf("Something went wrong writing to the database: %s", status)
				}
			}
		})
		log.Println("Connected to MQTT Broker...")
	}
}

func postDataToInflux(lineProtocol string) (int, string, error) {
	client := &http.Client{}
	buf := new(bytes.Buffer)
	buf.Write([]byte(lineProtocol))

	req, err := http.NewRequest("POST", writeUrl, buf)
	if err != nil {
		return 0, "", err
	}
	req.Header.Add(
		"Authorization",
		fmt.Sprintf(
			"Token %s:%s",
			cfg.InfluxDB.Username,
			cfg.InfluxDB.Password,
		),
	)

	body, err := client.Do(req)

	if err != nil {
		return 0, "", err
	}
	return body.StatusCode, body.Status, nil
}
