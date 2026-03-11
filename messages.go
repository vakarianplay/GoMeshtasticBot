package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lmatte7/gomesh"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

func main() {
	var radio gomesh.Radio
	var messagePayload string
	var messageSender uint32

	// Важно: передаём только IP, библиотека сама подключится к TCP :4403
	if err := radio.Init("192.168.88.45"); err != nil {
		log.Fatalf("init error: %v", err)
	}
	defer radio.Close()

	// Отправка сообщения:
	// to = 0  =&gt; broadcast
	// channel = 0 (обычно primary; если у тебя другой, поменяй)
	if err := radio.SendTextMessage("DM TEST", 1770314236, 0); err != nil {
		log.Fatalf("send error: %v", err)
	}
	log.Println("message sent")

	// Чтение входящих пакетов
	for {
		packets, err := radio.ReadResponse(true)
		if err != nil {
			log.Printf("read error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, fr := range packets {
			switch v := fr.GetPayloadVariant().(type) {
			case *pb.FromRadio_Packet:
				mp := v.Packet
				if mp == nil {
					continue
				}

				decoded := mp.GetDecoded()
				if decoded == nil {
					continue
				}

				// Ловим текстовые сообщения
				if decoded.Portnum == pb.PortNum_TEXT_MESSAGE_APP {
					fmt.Printf("[TEXT] from=%d id=%d to=%d ch=%d: %s\n", mp.GetFrom(), mp.GetId(), mp.GetTo(), mp.GetChannel(), string(decoded.GetPayload()))
					messagePayload = string(decoded.GetPayload())
					messageSender = mp.GetFrom()
				}

				if messagePayload == "/ping" {
					if err := radio.SendTextMessage("pong", int64(messageSender), 0); err != nil {
						log.Fatalf("send error: %v", err)
					}
					log.Println("answer send")
				}

				if messagePayload == "/info" {
					ansStr, _ := getInfoSting()
					if err := radio.SendTextMessage(ansStr, int64(messageSender), 0); err != nil {
						log.Fatalf("send error: %v", err)
					}
					log.Println("answer send")
				}

				messagePayload = ""

			case *pb.FromRadio_MyInfo:
				fmt.Printf("[MYINFO] myNodeNum=%d\n", v.MyInfo.GetMyNodeNum())

			default:
				// Можно оставить пусто, чтобы не шуметь логами
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

}

func getInfoSting() (string, error) {
	apiKey := "28bc310f78d0674674d5ca06e7a2a556"
	if apiKey == "" {
		return "", fmt.Errorf("empty apiKey")
	}
	url := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/weather?q=Elektrostal,RU&units=metric&lang=ru&appid=%s",
		apiKey,
	)
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("owm status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Dt       int64  `json:"dt"`
		Timezone int64  `json:"timezone"`
		Name     string `json:"name"`
		Main     struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Sys struct {
			Sunrise int64 `json:"sunrise"`
			Sunset  int64 `json:"sunset"`
		} `json:"sys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	locTime := func(ts int64) string {
		return time.Unix(ts+r.Timezone, 0).UTC().Format("2006-01-02 15:04")
	}
	desc := ""
	if len(r.Weather) > 0 {
		desc = r.Weather[0].Description
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Sprintf(
		"Сейчас: %s | %s: %.1f°C, влажн. %d%%, %s | Рассвет: %s | Закат: %s",
		now, r.Name, r.Main.Temp, r.Main.Humidity, desc,
		locTime(r.Sys.Sunrise), locTime(r.Sys.Sunset),
	), nil
}
