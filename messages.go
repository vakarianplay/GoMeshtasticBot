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

var nodeConfig nodeCfg

type nodeCfg struct {
	nodePort             string
	nodeInfoText         string
	openweathermapApiKey string
	openweathermapCity   string
	narodmonApiKey       string
	narodmonUuid         string
	narodmonSensorId     string
}

func main() {
	nodeConfig.nodePort,
		nodeConfig.nodeInfoText,
		nodeConfig.openweathermapApiKey,
		nodeConfig.openweathermapCity,
		nodeConfig.narodmonApiKey,
		nodeConfig.narodmonUuid,
		nodeConfig.narodmonSensorId = GetMeshConfig()

	RelayMesaage("=== MESH RELAYER START ===")

	attempt := 0
	for {
		started := time.Now()
		err := runBotSession(nodeConfig.nodePort)
		if err != nil {
			log.Printf("session ended: %v", err)
		}

		if time.Since(started) > 2*time.Minute {
			attempt = 0
		} else {
			attempt++
		}

		delay := reconnectDelay(attempt)
		log.Printf("connection lost, reconnect in %s...", delay)
		time.Sleep(delay)
	}
}

func reconnectDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 1 * time.Second
	case attempt == 2:
		return 2 * time.Second
	case attempt == 3:
		return 4 * time.Second
	case attempt == 4:
		return 8 * time.Second
	case attempt == 5:
		return 15 * time.Second
	default:
		return 30 * time.Second
	}
}

func runBotSession(port string) error {
	var radio gomesh.Radio
	var myNodeNum uint32

	if err := radio.Init(port); err != nil {
		return fmt.Errorf("init error: %w", err)
	}
	defer radio.Close()

	log.Printf("connected to node: %s", port)

	if err := nodeInfo(&radio); err != nil {
		log.Printf("nodeInfo error: %v", err)
	}

	if neighbours, err := getNeighbours(&radio); err != nil {
		log.Printf("getNeighbours error: %v", err)
	} else if err := SaveAllNeighboursToCSV(neighbours, "nodes.csv"); err != nil {
		log.Printf("save neighbours error: %v", err)
	} else {
		log.Println("Neighbours list update")
	}

	log.Println("===== B O T    S T A R T E D ======")

	neighTicker := time.NewTicker(10 * time.Minute)
	defer neighTicker.Stop()

	heartbeatTicker := time.NewTicker(20 * time.Second)
	defer heartbeatTicker.Stop()

	lastRx := time.Now()
	heartbeatErrors := 0

	for {
		// No-blocked reading
		packets, err := radio.ReadResponse(false)
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		if len(packets) > 0 {
			lastRx = time.Now()
		}

		for _, fr := range packets {
			switch v := fr.GetPayloadVariant().(type) {
			case *pb.FromRadio_MyInfo:
				myNodeNum = v.MyInfo.GetMyNodeNum()
				log.Printf("[MYINFO] myNodeNum=%d", myNodeNum)

			case *pb.FromRadio_Packet:
				mp := v.Packet
				if mp == nil {
					continue
				}
				decoded := mp.GetDecoded()
				if decoded == nil {
					continue
				}
				if decoded.GetPortnum() != pb.PortNum_TEXT_MESSAGE_APP {
					continue
				}

				text := strings.TrimSpace(string(decoded.GetPayload()))
				from := mp.GetFrom()
				to := mp.GetTo()
				ch := mp.GetChannel()

				shortName, fullName, _ := GetNames("nodes.csv", fmt.Sprint(from))

				log.Printf(
					"[TEXT] my=%d from=%d to=%d (0x%08X) ch=%d id=%d %s %s: %s",
					myNodeNum, from, to, to, ch, mp.GetId(),
					shortName, fullName, text,
				)

				info := fmt.Sprintf(
					"HOPS=%d   RSSI=%d dBm    SNR=%.1f dB",
					mp.GetHopStart()-mp.GetHopLimit(),
					mp.GetRxRssi(),
					mp.GetRxSnr(),
				)
				msgRelayer(text, shortName, fullName, fmt.Sprint(from), info)

				// Protection self-flood
				if myNodeNum != 0 && from == myNodeNum {
					continue
				}

				var reply string
				switch text {
				case "/ping":
					reply = buildPingReply(mp)
				case "/info":
					ans, err := getInfoSting()
					if err != nil {
						reply = "Ошибка получения данных"
					} else {
						reply = ans
					}
				case "/rates":
					reply = getRatesString()
				case "/radiation":
					reply = getRadiation()
				case "/nodeinfo":
					reply = nodeConfig.nodeInfoText
				case "/about":
					reply = "Meshtastic бот на golang. Разработка: https://vakarian.website\nРепозиторий: https://github.com/vakarianplay/GoMeshtasticBot"
				case "/help":
					reply = "/ping - пинг\n/info - погода\n/radiation - радиация\n/rates - курс валют\n/nodeinfo - о ноде\n/about - о боте"
				default:
					continue
				}

				isBroadcast := to == 0 || to == ^uint32(0)
				isLongFast := ch == 0
				isDM := !isBroadcast && (myNodeNum == 0 || to == myNodeNum)
				isPublicLongFast := isBroadcast && isLongFast

				switch {
				case isDM:
					if err := radio.SendTextMessage(reply, int64(from), 0); err != nil {
						log.Printf("send DM error: %v", err)
					} else {
						log.Printf("DM reply sent to %d", from)
					}

				case isPublicLongFast:
					if err := radio.SendTextMessage(reply, 0, int64(ch)); err != nil {
						log.Printf("send public error: %v", err)
					} else {
						log.Printf("public reply sent to channel=%d", ch)
					}

				default:
					log.Printf("skip: unknown route from=%d to=%d ch=%d", from, to, ch)
				}
			}
		}

		select {
		case <-neighTicker.C:
			neighbours, err := getNeighbours(&radio)
			if err != nil {
				return fmt.Errorf("getNeighbours failed: %w", err)
			}
			if err := SaveAllNeighboursToCSV(neighbours, "nodes.csv"); err != nil {
				log.Printf("save neighbours error: %v", err)
			} else {
				log.Println("Neighbours list update")
			}

		case <-heartbeatTicker.C:
			if time.Since(lastRx) > 40*time.Second {
				_, err := radio.GetRadioInfo()
				if err != nil {
					heartbeatErrors++
					log.Printf("heartbeat error (%d/3): %v", heartbeatErrors, err)
					if heartbeatErrors >= 3 {
						return fmt.Errorf("connection lost (heartbeat failed)")
					}
				} else {
					heartbeatErrors = 0
				}
			} else {
				heartbeatErrors = 0
			}

		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func buildPingReply(mp *pb.MeshPacket) string {
	rssi := mp.GetRxRssi()
	snr := mp.GetRxSnr()

	hopStart := int64(mp.GetHopStart())
	hopLimit := int64(mp.GetHopLimit())

	var hops int64
	if hopStart > 0 && hopStart >= hopLimit {
		hops = hopStart - hopLimit
	} else {
		hops = 0
	}

	return fmt.Sprintf(
		"📶 pong\n📶 RSSI: %d dBm | SNR: %.1f dB | hops: %d",
		rssi, snr, hops,
	)
}

func getInfoSting() (string, error) {
	apiKey := nodeConfig.openweathermapApiKey
	if apiKey == "" {
		return "", fmt.Errorf("empty apiKey")
	}

	url := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/weather?q=%s&units=metric&lang=ru&appid=%s",
		nodeConfig.openweathermapCity, apiKey,
	)

	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf(
			"owm status %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(b)),
		)
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
		"Сейчас: %s \n%s: %.1f°C, влажн. %d%%, %s | Рассвет: %s | Закат: %s",
		now, r.Name, r.Main.Temp, r.Main.Humidity, desc,
		locTime(r.Sys.Sunrise), locTime(r.Sys.Sunset),
	), nil
}

func getRatesString() string {
	client := &http.Client{Timeout: 6 * time.Second}

	usd := "n/a"
	eur := "n/a"
	btc := "n/a"
	eth := "n/a"
	trx := "n/a"
	ton := "n/a"

	if v, err := tinkoffPrices(client, "https://api.tinkoff.ru/v1/currency_rates?from=USD&to=RUB"); err == nil {
		usd = v
	}

	if v, err := tinkoffPrices(client, "https://api.tinkoff.ru/v1/currency_rates?from=EUR&to=RUB"); err == nil {
		eur = v
	}

	if c, err := binancePrices(client); err == nil {
		if v, ok := c["BTCUSDT"]; ok {
			btc = v
		}
		if v, ok := c["ETHUSDT"]; ok {
			eth = v
		}
		if v, ok := c["TRXUSDT"]; ok {
			trx = v
		}
		if v, ok := c["TONUSDT"]; ok {
			ton = v
		}
	}

	return fmt.Sprintf(
		"USD/RUB: %s | EUR/RUB: %s\nBTC/USD: %s | ETH/USD: %s | TRX/USD: %s | TON/USD: %s",
		usd, eur, btc, eth, trx, ton,
	)
}

func tinkoffPrices(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var d map[string]interface{}
	if err := json.Unmarshal(b, &d); err != nil {
		return "", err
	}

	payload, ok := d["payload"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("bad payload")
	}

	rates, ok := payload["rates"].([]interface{})
	if !ok || len(rates) == 0 {
		return "", fmt.Errorf("bad rates")
	}

	for _, rr := range rates {
		m, ok := rr.(map[string]interface{})
		if !ok {
			continue
		}
		if buy, ok := m["buy"]; ok {
			return fmt.Sprint(buy), nil
		}
	}

	return "", fmt.Errorf("buy not found")
}

func binancePrices(client *http.Client) (map[string]string, error) {
	u := `https://api.binance.com/api/v3/ticker/price?symbols=["BTCUSDT","ETHUSDT","TRXUSDT","TONUSDT"]`
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var d []map[string]interface{}
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(d))
	for _, v := range d {
		symbol, _ := v["symbol"].(string)
		price, _ := v["price"].(string)
		if symbol == "" {
			continue
		}
		out[symbol] = strings.TrimRight(strings.TrimRight(price, "0"), ".")
	}

	return out, nil
}

func getRadiation() string {
	apiKey := nodeConfig.narodmonApiKey
	if apiKey == "" {
		return "empty apiKey"
	}

	url := fmt.Sprintf(
		"http://api.narodmon.ru/sensorsOnDevice?id=%s&api_key=%s&uuid=%s&lang=ru",
		nodeConfig.narodmonSensorId, apiKey, nodeConfig.narodmonUuid,
	)

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "Narodmon bad request"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "Error read responce"
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "Json error"
	}

	sensors, ok := data["sensors"].([]interface{})
	if !ok {
		return "Narodmon data error"
	}

	for _, s := range sensors {
		sensor, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		if fmt.Sprint(sensor["name"]) == "Радиация" {
			return fmt.Sprintf("Радиация %v мкрР/час", sensor["value"])
		}
	}

	return "Датчик не найден"
}

func nodeInfo(radio *gomesh.Radio) error {
	responses, err := radio.GetRadioInfo()
	if err != nil {
		return err
	}

	var myNum uint32
	var myInfo *pb.FromRadio_MyInfo
	var myNode *pb.NodeInfo

	for _, r := range responses {
		if info, ok := r.GetPayloadVariant().(*pb.FromRadio_MyInfo); ok {
			myInfo = info
			myNum = info.MyInfo.MyNodeNum
		}
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			if myNum != 0 && ni.NodeInfo.Num == myNum {
				myNode = ni.NodeInfo
			}
		}
	}

	var metrics string
	var name string
	var userId string
	var model string
	if myNode != nil && myNode.User != nil && myNode.User.LongName != "" {
		name = myNode.User.LongName
		userId = myNode.User.Id
		metrics = myNode.DeviceMetrics.String()
		model = myNode.User.HwModel.String()
	} else if myNode != nil && myNode.User != nil {
		name = myNode.User.ShortName
	}

	fmt.Println("Node num: ", myNum)
	fmt.Println("ID: ", userId)
	fmt.Println("Name: ", name)
	fmt.Println("Hardware: ", model)
	fmt.Println("Metrics: ", metrics)
	if myInfo != nil && myInfo.MyInfo != nil {
		fmt.Println("Node info: ", myInfo.MyInfo.String())
	}

	return nil
}

func getNeighbours(radio *gomesh.Radio) (string, error) {
	var result string

	responses, err := radio.GetRadioInfo()
	if err != nil {
		return "", err
	}

	for _, r := range responses {
		if ni, ok := r.GetPayloadVariant().(*pb.FromRadio_NodeInfo); ok {
			result += fmt.Sprint(ni.NodeInfo)
		}
	}
	return result, nil
}

func msgRelayer(message, shortName, fullName, nodeId, info string) {
	firstString := "(" + shortName + ")  " + fullName + " | id: " + nodeId
	secondString := message
	thirdString := info

	fmt.Println(firstString, "\n\n", secondString, "\n\n", thirdString)
	RelayMesaage(fmt.Sprintf("📶 %s\n\n%s\n\n✔ %s",
		firstString, secondString, thirdString))
}
