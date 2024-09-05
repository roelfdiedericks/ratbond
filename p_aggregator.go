package main
import (
	"time"
	netip "net/netip"
	"os"
	"fmt"
	"encoding/json"

)


var g_mqtt_token=""
var g_mqtt_token_secret="testing"
var g_mqtt_argv=""

func do_mqtt_requests() {	
	//if we don't have a mqtt token yet, request it			
	if g_mqtt_token=="" {
		l.Warnf("AGGREGATOR: requesting mqtt token from aggregator: (%s)",g_mqtt_broker_addr)
		mqtt_send(fmt.Sprintf("/ratbond/%s/need-token",network_get_mac()),g_mqtt_token_secret)
	} else {

		//we have a token, now we can get to work....

		//if we don't have argvs request it
		if g_mqtt_argv=="" {
			l.Infof("AGGREGATOR: requesting argv...")
			mqtt_send(fmt.Sprintf("/ratbond/%s/need-argv",g_mqtt_token),"hello")
		} else {

			//is the client running ?
			if !g_run_client {			
				l.Infof("AGGREGATOR: argv:(%s) received from aggregator, starting client...",g_mqtt_argv)

				l.Tracef("AGGREGATOR: we got argvs! %s",g_mqtt_argv)
				var arr []string
				err := json.Unmarshal([]byte(g_mqtt_argv), &arr)
				if err!=nil {
					l.Errorf("argv json decoding error:%s",err)
					return;
				}

				l.Tracef("AGGREGATOR: decoded argvs! %+v",arr)
				os.Args=arr

				parse_cli()				
					
				
				go run_client()

			} 

		}
	}
}
func run_aggregator() {
	g_run_aggregator=true
	if g_mqtt_broker_addr=="" {
		l.Errorf("--mqtt-broker-addr is required")
		os.Exit(1)
	}
	l.Infof("AGGREGATOR: ratbond connectaggregator mqtt-server-addr: %s",g_mqtt_broker_addr)

	l.Infof("AGGREGATOR: connecting to aggregator")


	mqttaddr,err:=netip.ParseAddrPort(g_mqtt_broker_addr)
	if (err!=nil) {
		l.Errorf("AGGREGATOR: mqtt-broker-addr: %s error: %s",CLI.MqttBrokerAddr,err)
		os.Exit(1)
	}
	g_things_broker=mqttaddr.Addr().String()
	g_things_broker_port=int(mqttaddr.Port())



	mqtt_check_brokers()
	time.Sleep(100 * time.Millisecond) //wait for brokers to connect, otherwise we'll do it later
	if g_things_mqtt_connected {
		do_mqtt_requests()
	}
	
	loopcount := 1

	//TODO: ensure that each client has a mqtt token secret, to mash for a token
	// for now, we simply use the mac
	g_mqtt_token_secret=network_get_mac()
		
	for {
        time.Sleep(10 * time.Millisecond)

        loopcount++

        //debug loop every 2 seconds
        if loopcount%200 == 0 {
            l.Debugf("loop:%d \n", loopcount)
        }

		//reconnect to mqtt every 5 seconds, if not connected, and also process mqtt states
		if loopcount%200 == 0 {
			
			if g_things_mqtt_connected {
				do_mqtt_requests()
			} else {
				l.Infof("AGGREGATOR: waiting for mqtt connection to:%s",g_mqtt_broker_addr)
			}
		}

		
		//recycle every 15 seconds, do occasional stuff
		if loopcount%1500 == 0 {
            l.Tracef("loop:%d, housekeeping", loopcount)
			mqtt_check_brokers()
			
			if g_run_client {
					//the client is running
					//simply send a hello every now and again, with our stats
					stats:=printServerList(g_server_list)
					l.Debugf("AGGREGATOR:mqtt hello, sending stats...")
					l.Tracef("AGGREGATOR:mqtt hello, sending stats: %s",stats)
					mqtt_send(fmt.Sprintf("/ratbond/%s/hello",g_mqtt_token),stats)
			}
            loopcount = 1
        }

	}

}