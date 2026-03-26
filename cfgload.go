package main

import (
	"fmt"
	"io/ioutil"
	"log"

	yaml "gopkg.in/yaml.v3"
)

func readCfg() []string {

	var cfgYaml map[string]interface{}
	cfgFile, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(cfgFile, &cfgYaml)

	if err != nil {
		log.Fatal(err)
	}

	port := (cfgYaml["mesh"].(map[string]interface{})["port"])
	nodeInfoText := (cfgYaml["mesh"].(map[string]interface{})["node_info_text"])
	openweathermapApiKey := (cfgYaml["mesh"].(map[string]interface{})["openweathermap_api_key"])
	openweathermapCity := (cfgYaml["mesh"].(map[string]interface{})["openweathermap_city"])
	narodmonApiKey := (cfgYaml["mesh"].(map[string]interface{})["narodmon_api_key"])
	narodmonUuid := (cfgYaml["mesh"].(map[string]interface{})["narodmon_uuid"])
	narodmonSensorId := (cfgYaml["mesh"].(map[string]interface{})["narodmon_sensor_id"])

	homeserver := (cfgYaml["matrix-relay"].(map[string]interface{})["homeserver"])
	username := (cfgYaml["matrix-relay"].(map[string]interface{})["username"])
	password := (cfgYaml["matrix-relay"].(map[string]interface{})["password"])
	device_id := (cfgYaml["matrix-relay"].(map[string]interface{})["device_id"])
	target_room := (cfgYaml["matrix-relay"].(map[string]interface{})["matrixRoomID"])
	enabled_matrix := (cfgYaml["matrix-relay"].(map[string]interface{})["enabled"])

	telegramBotToken := (cfgYaml["telegram-relay"].(map[string]interface{})["botToken"])
	telegramChatId := (cfgYaml["telegram-relay"].(map[string]interface{})["chatID"])
	enabled_telegram := (cfgYaml["telegram-relay"].(map[string]interface{})["enabled"])

	enabled_custom := (cfgYaml["custom-relay"].(map[string]interface{})["enabled"])
	custom_relay_url := (cfgYaml["custom-relay"].(map[string]interface{})["url"])
	custom_relay_auth_token := (cfgYaml["custom-relay"].(map[string]interface{})["authToken"])

	port_ := fmt.Sprintf("%v", port)
	nodeInfoText_ := fmt.Sprintf("%v", nodeInfoText)
	openweathermapApiKey_ := fmt.Sprintf("%v", openweathermapApiKey)
	openweathermapCity_ := fmt.Sprintf("%v", openweathermapCity)
	narodmonApiKey_ := fmt.Sprintf("%v", narodmonApiKey)
	narodmonUuid_ := fmt.Sprintf("%v", narodmonUuid)
	narodmonSensorId_ := fmt.Sprintf("%v", narodmonSensorId)

	homeserver_ := fmt.Sprintf("%v", homeserver)
	username_ := fmt.Sprintf("%v", username)
	password_ := fmt.Sprintf("%v", password)
	device_id_ := fmt.Sprintf("%v", device_id)
	target_room_ := fmt.Sprintf("%v", target_room)
	enabled_matrix_ := fmt.Sprintf("%v", enabled_matrix)
	telegramBotToken_ := fmt.Sprintf("%v", telegramBotToken)
	telegramChatId_ := fmt.Sprintf("%v", telegramChatId)
	enabled_telegram_ := fmt.Sprintf("%v", enabled_telegram)

	enabled_custom_ := fmt.Sprintf("%v", enabled_custom)
	custom_relay_url_ := fmt.Sprintf("%v", custom_relay_url)
	custom_relay_auth_token_ := fmt.Sprintf("%v", custom_relay_auth_token)

	var out []string
	out = append(out, homeserver_, username_, password_, device_id_, target_room_, enabled_matrix_, telegramBotToken_, telegramChatId_, enabled_telegram_, port_, nodeInfoText_, openweathermapApiKey_, openweathermapCity_, narodmonApiKey_, narodmonUuid_, narodmonSensorId_, enabled_custom_, custom_relay_url_, custom_relay_auth_token_)

	// fmt.Println(out)
	return out
}

func GetMatrixConfig() (string, string, string, string, string, string) {
	cfg := readCfg()
	return cfg[0], cfg[1], cfg[2], cfg[3], cfg[4], cfg[5]
}

func GetTelegramConfig() (string, string, string) {
	cfg := readCfg()
	return cfg[6], cfg[7], cfg[8]
}

func GetMeshConfig() (string, string, string, string, string, string, string) {
	cfg := readCfg()
	return cfg[9], cfg[10], cfg[11], cfg[12], cfg[13], cfg[14], cfg[15]
}

func GetCustomRelayConfig() (string, string, string) {
	cfg := readCfg()
	return cfg[16], cfg[17], cfg[18]
}
