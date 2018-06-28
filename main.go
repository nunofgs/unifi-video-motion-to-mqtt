package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	tail "github.com/hpcloud/tail"
	strcase "github.com/iancoleman/strcase"
	viper "github.com/spf13/viper"
)

// Configuration from file
type Configuration struct {
	MQTT struct {
		Broker   string
		Username string
		Password string
	}
}

// Device for auto discovery
type Device struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	AvailabilityTopic string `json:"availability_topic"`
	StateTopic        string `json:"state_topic"`
	DeviceClass       string `json:"device_class"`
	PayloadOn         string `json:"payload_on"`
	PayloadOff        string `json:"payload_off"`
}

// Regular expression for motion start/end log messages
var match = regexp.MustCompile(`^Parsed id: (\w+), name: ([\w\s]+), action: (STARTED|ENDED), motion: (\d+.*), recording: ([a-z0-9]+\b|null)`)

// Retrieve configuration from file
func getConfiguration() (Configuration, error) {
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	config := Configuration{}

	if err := viper.ReadInConfig(); err != nil {
		return config, err
	}

	if err := viper.Unmarshal(&config); err != nil {
		return config, err
	}

	if config.MQTT.Broker == "" {
		return config, fmt.Errorf("Missing `mqtt.broker` parameter in configuration")
	}

	return config, nil
}

// Connect and return an MQTT client
func getMqttClient(options *mqtt.ClientOptions) (mqtt.Client, error) {
	client := mqtt.NewClient(options)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return client, nil
}

func main() {
	config, err := getConfiguration()

	if err != nil {
		log.Fatalf("Could not read config file: %s", err)
	}

	file := flag.String("file", "", "path to the recording.log file")

	flag.Parse()

	if *file == "" {
		flag.Usage()
		return
	}

	// Seek to end of file and tail it (using inotify)
	location := &tail.SeekInfo{Whence: os.SEEK_END}

	t, err := tail.TailFile(*file, tail.Config{Follow: true, Location: location, Logger: tail.DiscardingLogger, MustExist: true})

	if err != nil {
		log.Fatal(err)
	}

	for line := range t.Lines {
		// Retrieve matches for our regexp
		matches := match.FindStringSubmatch(line.Text)

		if len(matches) == 0 {
			continue
		}

		id, name, action, motion, recording := matches[1], matches[2], matches[3], matches[4], matches[5]
		topic := fmt.Sprintf("homeassistant/binary_sensor/%s_motion", strcase.ToSnake(name))

		fmt.Printf("Parsed id: %s, name: %s, action: %s, motion: %s, recording: %s\n", id, name, action, motion, recording)

		device := Device{
			ID:                id,
			Name:              fmt.Sprintf("%s Camera Motion", name),
			AvailabilityTopic: fmt.Sprintf("%s/%s", topic, "status"),
			StateTopic:        fmt.Sprintf("%s/%s", topic, "state"),
			DeviceClass:       "motion",
			PayloadOn:         "STARTED",
			PayloadOff:        "ENDED",
		}

		options := mqtt.NewClientOptions()
		options.AddBroker(config.MQTT.Broker)
		options.SetUsername(config.MQTT.Username)
		options.SetPassword(config.MQTT.Password)
		options.SetWill(device.AvailabilityTopic, "offline", 0, false)

		// Connect to MQTT
		client, err := getMqttClient(options)

		if err != nil {
			log.Fatal(err)
		}

		jsonDevice, err := json.Marshal(device)

		if err != nil {
			log.Fatalf("Error marshalling JSON: %s", err)
		}

		// Publish device config for autodiscovery
		if token := client.Publish(fmt.Sprintf("%s/%s", topic, "config"), 0, false, string(jsonDevice)); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
		}

		// Publish motion state to MQTT
		if token := client.Publish(device.StateTopic, 0, false, action); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
		}

		// Publish device status as online
		if token := client.Publish(device.AvailabilityTopic, 0, false, "online"); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
		}
	}
}
