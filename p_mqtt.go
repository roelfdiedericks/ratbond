package main
import (
	"fmt"
	"time"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var g_things_broker string = "127.0.0.1"
var g_things_mqttclient mqtt.Client
var g_things_mqtt_connected bool = false

var things_connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	l.Infof("THINGS:Connected to %s", g_things_broker)
	g_things_mqtt_connected = true
	mqtt_sub(g_things_mqttclient)
}

var things_connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	l.Warnf("THINGS:Connect lost: %v", err)
	g_things_mqtt_connected = false
}

var things_messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	len := int64(len(msg.Payload()))
	hl := ByteCountDecimal(len) //human length
	l.Infof("THINGS:Received message: len(%s) from topic:%s\n", hl, msg.Topic())
	switch msg.Topic() {
	case "ratbond/something":
		ratbond_something(msg)
		l.Errorf("something: %s payload:%s", msg.Topic(), msg.Payload())
	default:
		l.Errorf("unhandled topic: %sm payload:%s", msg.Topic(), msg.Payload())
	}
}


func ratbond_something(msg mqtt.Message) {
	l.Warnf("ratbond_something:%s", string(msg.Payload()))
}


func mqtt_connect_things() bool {
	var port = 1883
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", g_things_broker, port))
	opts.SetClientID("")
	opts.SetUsername("ratbond")
	opts.SetPassword("ratbond")
	opts.SetDefaultPublishHandler(things_messagePubHandler)
	opts.SetPingTimeout(time.Second * 5)
	opts.SetResumeSubs(true)
	opts.OnConnect = things_connectHandler
	opts.OnConnectionLost = things_connectLostHandler
	l.Infof("THINGS:mqtt connecting to:%s", g_things_broker)
	g_things_mqttclient = mqtt.NewClient(opts)
	if token := g_things_mqttclient.Connect(); token.Wait() && token.Error() != nil {
		l.Error(token.Error())
		time.Sleep(time.Second * 1)
		return false
	}
	return true
}


func mqtt_check_brokers() {
	l.Infof("BROKERS: things:%t", g_things_mqtt_connected)

	if !g_things_mqtt_connected {
		g_things_mqtt_connected=false
		mqtt_connect_things()
	}
}

func mqtt_sub(client mqtt.Client) {
	topic := "ratbond/something"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	l.Infof("Subscribed to %s\n", topic)
}


/* --------------------------------------------------------------------------------------------------------- */

func publishersub(client mqtt.Client) {

	topic := "ratbond/command"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	l.Infof("Subscribed to %s\n", topic)

}