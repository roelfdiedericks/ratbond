package main
import (
	"fmt"
	"time"
	"os"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var g_things_broker string = ""
var g_things_broker_port int = 1833
var g_things_mqttclient mqtt.Client
var g_things_mqtt_connected bool = false

var things_connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	l.Infof("MQTT:Connected to AGGREGATOR: %s:%d", g_things_broker,g_things_broker_port)
	g_things_mqtt_connected = true
	g_mqtt_token=""
	//subscribe to topics we need
	mqtt_sub(g_things_mqttclient, fmt.Sprintf("/ratbondserver/%s/token",network_get_mac()))
	do_mqtt_requests()
}

var things_connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	l.Warnf("MQTT:Connect lost: %v", err)
	g_things_mqttclient.Disconnect(1000);
	g_things_mqtt_connected = false
	g_things_mqttclient=nil
	g_mqtt_token=""
}

var things_messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	len := int64(len(msg.Payload()))
	hl := ByteCountDecimal(len) //human length
	l.Debugf("MQTT:Received message: len(%s) from topic:%s\n", hl, msg.Topic())

	//our topics of interest
	token_topic:=fmt.Sprintf("/ratbondserver/%s/token",network_get_mac())
	

	if  msg.Topic()==token_topic {
		l.Infof("AGGREGATOR: received a token: %s",msg.Payload())
		g_mqtt_token=fmt.Sprintf("%s",msg.Payload())

		//subscribe to our topics of interest with the token
		topic:= fmt.Sprintf("/ratbondserver/%s/+",g_mqtt_token)
		mqtt_sub(g_things_mqttclient,topic)
		return
	} else {
		l.Debugf("topic: %s!=%s",msg.Topic(),token_topic)
	}

	//receive topics from the broker, ideally a switch statement, but....

	//ARGV topic
	argv_topic:= fmt.Sprintf("/ratbondserver/%s/argv",g_mqtt_token)
	if msg.Topic()==argv_topic {
		l.Debugf("received argv:%s",msg.Payload())
		received_argv:=string(msg.Payload())
		if g_mqtt_argv!="" && g_mqtt_argv!=received_argv {
			//TODO: this could be more graceful, but ....
			l.Errorf("client argvs have changed, exiting to apply...")			
			os.Exit(1)
		}
		g_mqtt_argv=string(msg.Payload())
		return
	}
	
	
	l.Errorf("unhandled topic: %s payload:%s", msg.Topic(), msg.Payload())
}


func ratbond_something(msg mqtt.Message) {
	l.Warnf("ratbond_something:%s", string(msg.Payload()))
}


func mqtt_connect_things() bool {
	if (g_things_mqttclient!=nil) {
		g_things_mqttclient.Disconnect(1000)
	}
	g_things_mqttclient=nil
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", g_things_broker, g_things_broker_port))
	opts.SetClientID("")
	opts.SetUsername(g_mqtt_username)
	opts.SetPassword(g_mqtt_password)
	opts.SetDefaultPublishHandler(things_messagePubHandler)
	opts.SetPingTimeout(time.Second * 5)
	opts.SetResumeSubs(true)
	opts.OnConnect = things_connectHandler
	opts.OnConnectionLost = things_connectLostHandler
	l.Debugf("MQTT:connecting to %s:%d", g_things_broker,g_things_broker_port)
	g_things_mqttclient = mqtt.NewClient(opts)
	if token := g_things_mqttclient.Connect(); token.Wait() && token.Error() != nil {
		l.Error(token.Error())
		time.Sleep(time.Second * 1)
		return false
	}
	return true
}


func mqtt_check_brokers() {
	l.Debugf("connected:%t", g_things_mqtt_connected)

	if !g_things_mqtt_connected {
		g_things_mqtt_connected=false
		l.Warnf("mqtt not connected")
		mqtt_connect_things()
	}
}

func mqtt_sub(client mqtt.Client, topic string) {
	if g_things_mqtt_connected && client!=nil {
		token := client.Subscribe(topic, 1, nil)
		token.Wait()
		l.Infof("Subscribed to %s\n", topic)
	}
}

func mqtt_send(topic string, payload string) {
	if g_things_mqtt_connected && g_things_mqttclient !=nil {
		hl := ByteCountDecimal(int64(len(payload))) //human length
		l.Debugf("sending: len(%s) to (%s)",hl, topic)
		token := g_things_mqttclient.Publish(topic, 0, false, payload)
		token.Wait()
		l.Debugf("requested %s",topic)
	}
}


