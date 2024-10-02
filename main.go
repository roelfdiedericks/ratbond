package main

import (
	"os"
	"fmt"
	"net"
	netip "net/netip"

	"time"
	"encoding/binary"
	"sync"
	"strconv"
	"strings"


	"github.com/pkg/errors"


	"github.com/alecthomas/kong"

	"github.com/songgao/water"

	"golang.org/x/net/ipv4"


	

	
	"stathat.com/c/consistent"
	_ "net/http/pprof"
	parsetcp "github.com/ilyaigpetrov/parse-tcp-go"
	"github.com/carlmjohnson/versioninfo"

)

//operating mode
var g_run_client=false
var g_run_server=false
var g_run_aggregator=true
var g_use_syslog=true
var g_debug bool = false
var g_trace bool = false
var g_use_consistent_hashing = true

var g_connect_addr = ""
var g_connect_ip = ""
var g_listen_addr = "0.0.0.0:12345"

var g_mqtt_broker_addr = ""
var g_mqtt_username=""
var g_mqtt_password=""



var g_buffer_size = 2165535
var g_tunnel_id uint32=0
var g_tunnel_name=""
var g_server_tun_ip string ="10.10.10.2"
var g_client_tun_ip string ="10.10.10.1"
var g_http_listen_addr = "0.0.0.0:8091"
var g_secret="ratbond"
var g_use_aes=false

var g_mux_max=1

const g_write_deadline=2000
const g_client_max_rxtimeouts=3
const g_client_max_mss_probes=200
const g_client_mss_increment=4
const g_max_hello=5




var g_server_mss int=1472-g_udp_overhead //works in general for a 1500 byte frame

var g_client_mss int=1360-g_udp_overhead // works on pretty much everything, we will start probing upwards from here...

var g_max_mss=1500-28 //udp cannot possibly operate above this (1500 ethernet-28 IP+UDP) == 1472

var g_tunnel_mtu=1200

var g_reorder_buffer_size=128

var g_client_hello = []byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00HELLO\nFROMCLIENT")
var g_server_hello = []byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00HELLO\nFROMSERVER")

var g_data string



type packet struct {
	sequence uint64
	buff [1500]byte
}



/* ----------------------------------------------------*/
/* structs for the server, dealing with clients conns  */
/* ----------------------------------------------------*/
type clientConnection struct {
	convid uint32
	session *RatSession

	rxloss uint32
	txloss uint32
	txcounter uint64
	txbytes uint64
	rxcounter uint64
	rxbytes uint64
	rxtimeouts uint64
	txtimeouts uint64
	priority uint32

	last_bw_update time.Time
	txbandwidth float32
	rxbandwidth float32
	
	
	last_hello time.Time
	up_since time.Time
	alive bool
	src_address string

	mss int
}

type clientType struct {
	mu  *sync.Mutex
	connections map[uint32] *clientConnection
	consistent *consistent.Consistent
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	my_tun_ip string
	remote_tun_ip string
	reorder_buffer []packet
}

var g_client_list =make(map[uint32] *clientType)




/* --------------------------------------------------*/
/* structs for the client, dealing with serverconns  */
/* --------------------------------------------------*/
type serverConnection struct {
	convid uint32
	session *RatSession

	ifname string
	rxloss uint32
	txloss uint32
	txcounter uint64
	txbytes uint64
	rxcounter uint64
	rxbytes uint64
	rxtimeouts uint64
	txtimeouts uint64
	priority uint32

	last_bw_update time.Time
	txbandwidth float32
	rxbandwidth float32

	last_hello time.Time
	up_since time.Time
	alive bool
	src_address string
	wan_ip string

	ping_seq int
	mss int
	next_mss int //next mss to try
	settled_mss int //mss settled on
	mss_is_settled bool
	probe_count int //number of probes sent
	probe_acks int //number of acknowledge probes
}

type serverType struct {
	mu  *sync.Mutex
	connections map[uint32] *serverConnection
	consistent *consistent.Consistent
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	my_tun_ip string
	remote_tun_ip string

	reorder_buffer []packet
}

var g_server_list =make(map[uint32] *serverType)





func client_num_connections_by_src(src string,server *serverType) (int) {
	cnt:=0
	for _ , server_connection := range server.connections {
		if server_connection.src_address==src {
			cnt++
		}
	}	
	return cnt
}


func client_connect_server(tunnelid uint32, src string, ifname string, gw string, reason string) {
	
	

	server,ok:=g_server_list[tunnelid]
	if !ok {
		server=new(serverType)
		server.mu=new(sync.Mutex)
		g_server_list[tunnelid]=server
	}
		
	server.mu.Lock()

	//check how many connections with this src exists
	num:=client_num_connections_by_src(src,server)
	if num>=g_mux_max {
		l.Debugf("enough connections with src:%s, muxcount:%d",src,num);
		server.mu.Unlock()
		return
	}

	l.Warnf("connecting to server (%s), base tunnelid:%d, src:%s, ifname:%s gw:%s",reason,tunnelid,src,ifname,gw)


	

	//if there's no interface, create the tun for them
	spawn_tun:=false
	if (server.iface == nil) {		
		iface,err:=createTun(tunnelid,g_client_tun_ip)
		if err!=nil {
			l.Errorf("error creating tun%d interface :%s",tunnelid,err)
		}
		server.iface=iface
		server.my_tun_ip=g_client_tun_ip
		server.remote_tun_ip=g_server_tun_ip
		server.ifname=iface.Name()
		server.base_convid=tunnelid	


		//add default route via the tunnel
		//deprected: we now do it in the loop, when there are enough connections
		//this might also sort out connecting via private IP's over the tunnel issue
		//runthing("ip","route","add","default","via",server.remote_tun_ip)//
		spawn_tun=true
	}

	l.Info("tun created")

	//create the map if required
	if server.connections==nil {
		server.connections=make(map[uint32] *serverConnection)
		server.consistent = consistent.New()
	}
	

	//create the reorder buffer if required
	if (server.reorder_buffer==nil) {
		
		//server.reorder_buffer=make([]packet,g_reorder_buffer_size)
		//l.Warnf("created reorder buffer size:%d",g_reorder_buffer_size)
	}

	server.mu.Unlock()
	


	
	//find an open slot 0-9 on the base tunnelid, and start a connection
	var avail uint32
	for avail=tunnelid; avail<=tunnelid+10; avail++ {
		_, ok := server.connections[avail]
		if !ok {
			//we can use this convid
			break;
		}
	}
	if avail==tunnelid+100 {
		l.Errorf("no more additional connections available for tunnel: %d",tunnelid)
		return
	}
	l.Infof("found available convid:%d",avail)

	l.Infof("obtaining WAN ip for %s...",ifname)
	wan_ip,err:=get_wan_ip(ifname) 
	if err!=nil {
		l.Warnf("unable to get WAN ip: %s",err)
	}
	
	l.Infof("%s wan ip: %s",ifname,wan_ip)


	server.mu.Lock()
	server.last_convid=0 //reset the scheduler

	//add the new connection the the list
	var server_connection=new(serverConnection)
	server.connections[avail]=server_connection

	//init the struct
	server_connection.txbandwidth=0; 
	server_connection.rxbandwidth=0; 
	server_connection.last_bw_update=time.Now()
	server_connection.rxloss=0; server_connection.txloss=0; server_connection.alive=true; 
	server_connection.last_hello=time.Now()
	server_connection.up_since=time.Now()
	server_connection.txcounter=0
	server_connection.txbytes=0
	server_connection.rxcounter=0
	server_connection.rxbytes=0
	server_connection.rxtimeouts=0
	server_connection.txtimeouts=0
	server_connection.priority=0
	server_connection.convid=avail
	server_connection.src_address=src
	server_connection.wan_ip=wan_ip
	server_connection.ifname=ifname
	server_connection.ping_seq=1
	server_connection.mss=g_client_mss //start off with our generic mss
	server_connection.next_mss=g_client_mss //start off with our generic mss
	server_connection.settled_mss=0
	server_connection.mss_is_settled=false
	server_connection.probe_count=0
	server_connection.probe_acks=0


	

	session, err := create_session(avail,src)
	if err != nil {
		l.Panicf("session conn create error:", err)
	}
	l.Infof("session created: %+v",session)
	if err := session.SetReadBuffer(g_buffer_size); err != nil {
		l.Errorf("SetReadBuffer:", err)
	}
	if err := session.SetWriteBuffer(g_buffer_size); err != nil {
		l.Errorf("SetWriteBuffer:", err)
	}

	server_connection.session=session	
	server_connection.session.SetMss(server_connection.mss)

	g_server_list[tunnelid]=server
	server.consistent.Add( fmt.Sprintf("%d",avail) )
	server.mu.Unlock()

	l.Debugf("g_server_list: \n%s",printServerList(g_server_list))

	session.SendHello(); // this wakes up the server with out convid

	go client_handle_session(server,server_connection)
	if (spawn_tun) {
		go client_handle_tun(server)
	}

	client_check_paths()
}


func client_close_connection(server *serverType, server_connection *serverConnection,convid uint32) {
	server_connection.session.Close(); server_connection.session=nil
	server.last_convid=0 //restart the round robin			
	server.consistent.Remove(fmt.Sprintf("%d",convid))
	delete(server.connections,convid)
}


func client_disconnect_session_by_ifname(tunnelid uint32, ifname string) {
	l.Warnf("INTERFACE %s DOWN! DISCONNECTING from server, base tunnelid:%d, ifname:%s",ifname,tunnelid,ifname)
	
	server,ok := g_server_list[tunnelid]
	if !ok || server==nil {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
		l.Errorf("g_server_list: \n%s",printServerList(g_server_list))
		return
	}

	server.mu.Lock()
	//find the kcp connection matching the ifname and disconnect/destroy it
	for convid, connection := range server.connections {
		if connection.ifname==ifname {
			l.Infof("DISCONNECTING kcp convid: %d",connection.convid)			
			client_close_connection(server,connection,convid)
			client_send_linkdown_message(tunnelid,convid,fmt.Sprintf("interface: %s went down",ifname))
		}
	}	
	server.mu.Unlock()

	
	l.Warnf("disconnected session_by_ifname tunnelid:%d, ifname:%s",tunnelid,ifname)
}

func client_disconnect_session_by_src(tunnelid uint32, src string, ifname string, gw string) {
	l.Debugf("DISCONNECTING from server, base tunnelid:%d, src:%s, ifname:%s gw:%s",tunnelid,src,ifname,gw)
	
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
	}

	server.mu.Lock()
	//find the kcp connection matching the src address and disconnect/destroy it
	for convid, connection := range server.connections {
		if connection.src_address==src {
			l.Errorf("disconnecting kcp convid: %d, ifname:%s",connection.convid,connection.ifname)
			client_close_connection(server,connection,convid)			
			client_send_linkdown_message(tunnelid,convid,fmt.Sprintf("source:%s src default route removed"))
		}
	}	
	server.mu.Unlock()


	l.Warnf("disconnected session_by_src tunnelid:%d, src:%s",ifname,src)
}

func client_disconnect_session_by_convid(tunnelid uint32, disc_convid uint32, reason string) {
	l.Errorf("client disconnect tunnelid:%d, convid:%d, reason:%s",tunnelid,disc_convid,reason)
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
	}
	
	//find the kcp connection matching the src address and disconnect/destroy it
	server.mu.Lock()
	for convid, connection := range server.connections {
		if connection.convid==disc_convid {
			l.Errorf("disconnecting kcp convid: %d ifname:%s",connection.convid,connection.ifname)
			client_close_connection(server,connection,convid)					
			client_send_linkdown_message(tunnelid,convid,reason)
		}
	}	
	server.mu.Unlock()

	l.Warnf("disconnected session_by_convid tunnelid:%d, convid:%d",tunnelid,disc_convid)
}


func client_send_linkdown_message(tunnelid uint32, disc_convid uint32, reason string) {
	l.Errorf("client send LINKDOWN(%s) for tunnelid:%d, convid:%d",reason,tunnelid,disc_convid)
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
	}
	//choose a connection that is still up
	chose_convid,err:=client_choose_conn(nil,0,server)
	if err!=nil || chose_convid==0 {
		l.Debugf("unable to find a valid connection to send linkdown with")
		return
	}
	connection,ok:=server.connections[chose_convid] 
	if connection==nil && !ok{
		l.Debugf("unable to find a valid connection to send linkdown with")
		return
	}

	//create the message
	pkt:=[]byte{0,0,0}
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(disc_convid))
	pkt[1]=b[0]
	pkt[2]=b[1]

	//and send it out the selected KCP conn
	l.Infof("send LINKDOWN via convid:%d sending via %s",chose_convid,connection.src_address)
	
	connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*g_write_deadline)) 
	_, err = connection.session.Write(pkt)
	if err != nil {
		l.Errorf("unable to send linkdown message:", err)		
		return
	}
	connection.txcounter++
	connection.txbytes+=uint64(len(b))
}


func client_check_paths() {
	//TODO: check if we have the route first ?
	//TODO: is this the best way, may cause TCP connection resets if we remove routes....
	if g_server_list[g_tunnel_id]!=nil {
		g_server_list[g_tunnel_id].mu.Lock()
		server:=g_server_list[g_tunnel_id]


		if (len(server.connections)>0) {
			//check if the route exists
			check:=runthing("ip","route","show","match","0.0.0.0")
			l.Debugf("check default route:%s",check)

			if strings.Contains(check,server.remote_tun_ip) {
				l.Debugf("default route via tunnel exists")
			} else {
				l.Warnf("TUNNELUP!: adding default route via %s",server.remote_tun_ip)
				runthing("ip","route","add","default","via",server.remote_tun_ip)
				
			}
		} else {
			check:=runthing("ip","route","show","match","0.0.0.0")
			l.Debugf("ALLDOWN: check default route:%s",check)
			if strings.Contains(check,server.remote_tun_ip) {
				l.Warnf("removing default route via %s",server.remote_tun_ip)
				runthing("ip","route","delete","default","via",server.remote_tun_ip)
			} else {
				l.Debugf("default route via tunnel doesnt exist, not removing")
			}
		}
		g_server_list[g_tunnel_id].mu.Unlock()
	}
}


func run_client() {

	if g_connect_addr=="" {
		l.Errorf("--connect-addr is required in client mode")
		os.Exit(1)
	}
	if g_tunnel_id==0 {
		l.Errorf("--tunnel-id is required in client mode")
		os.Exit(1)
	}
	
	l.Infof("ratbond client connect-addr: %s",g_connect_addr)
	if (CLI.TunName!="") {
		g_tunnel_name=CLI.TunName
		l.Infof("using tun-name:%s",g_tunnel_name)
	}

	go http_serve()


	g_run_client=true

	l.Infof("running as client, base tunnel id:%d",g_tunnel_id)
	
	
	//create two connections, deprecated, handled by netlink_subscribe_routes
	//client_connect_server(g_client_tunnelid)
	//client_connect_server(g_client_tunnelid)

	//mqtt_check_brokers()
	loopcount := 1

	runthing("ip", "-br", "address")

	go netlink_subscribe_ifaces(g_tunnel_id)

	// this netlink listener monitors kernel routes, and will connect to the server once a default route is seen,
	// by calling client_connect_server with the details of the route that came up.
	//
	// this is how all connections to a server is established. At startup all existing route information
	// is read by netlink_subscribe_ifaces, so that existing connections are used and initiated.
	go netlink_subscribe_routes(g_tunnel_id)

	for {
        time.Sleep(10 * time.Millisecond)

        loopcount++

        //debug loop every 2 seconds
        if loopcount%200 == 0 {
            l.Tracef("loop:%d \n", loopcount)
        }


  		//reconnect to mqtt every 5 seconds, if not connected
  		if loopcount%500 == 0 {
			
			//add/remove the default route every 5 seconds, depending on our connections to the server
			client_check_paths()
		}

		//discover mss
		if loopcount%20 == 0 {
			client_probe_mss()
		}
		
		//send ping to each server kcp every 2 seconds
		if loopcount%200 == 0 {
			client_send_server_pings()
		}

		
		//recycle every 15 seconds, do occasional stuff
        if loopcount%1500 == 0 {
            l.Tracef("loop:%d, housekeeping", loopcount)
            loopcount = 1
			l.Debugf("g_server_list: \n%s",printServerList(g_server_list))
			l.Tracef("loads: \n %s",printServerLoads(g_server_list))
			

			//we poll the routing table every now and again, in case we missed or state messed us arround
			//this allows us to reconnect to the server every so often
			go netlink_get_routes(g_tunnel_id)

			
        }

	}
}



func run_server() {
	g_run_server=true

	l.Infof("ratbond server listen-addr: %s",g_listen_addr)

	if g_tunnel_id==0 {
		l.Errorf("--tunnel-id is required in server mode")
		os.Exit(1)
	}

	if (CLI.TunName!="") {				
		l.Warnf("tun-name:%s is not used on the server, tun interface is based on tunnel-id",g_tunnel_name)
	}

	go http_serve()

	l.Infof("running as server")

	//go netlink_subscribe_ifaces()

	var err error
	
	g_listener, err = createListener(g_listen_addr)
	l.Debugf("listener:%+v",g_listener)
	if err != nil {
		fmt.Println("listener create err:", err)
		return
	}

	if err := g_listener.SetReadBuffer(g_buffer_size); err != nil {
		l.Errorf("SetReadBuffer:", err)
	}
	if err := g_listener.SetWriteBuffer(g_buffer_size); err != nil {
		l.Errorf("SetWriteBuffer:", err)
	}
	
	

	go server_listen_udp(g_tunnel_id)
	//go server_listen_tun(iface)


	//mqtt_check_brokers()
	loopcount := 1

	runthing("ip", "-br", "address")

	
	for {
        time.Sleep(10 * time.Millisecond)

        loopcount++

        //debug loop every 2 seconds
        if loopcount%200 == 0 {
            l.Debugf("loop:%d \n", loopcount)
        }
  		//reconnect to mqtt every 5 seconds, if not connected
  		if loopcount%500 == 0 {
			//mqtt_check_brokers()
		}

		//send ping to each client kcp every 2 seconds
		if loopcount%200 == 0 {
			server_send_client_pings()
		}

		

		//recycle every 15 seconds, do occasional stuff
		if loopcount%1500 == 0 {
            l.Tracef("loop:%d, housekeeping", loopcount)
			//l.Warnf("g_client_list: %+v",g_client_list)
			l.Debugf("g_client_list: \n%s",printClientList(g_client_list))
			l.Tracef("loads: \n %s",printClientLoads(g_client_list))

			g_remoteConns.Range(func(key, value interface{}) bool {
				l.Infof("remoteConn:%s",key.(string))
				return true
			})
            loopcount = 1
        }

	}

}


func create_session(convid uint32, src string) (*RatSession, error) {
	l.Infof("connecting to %s: convid:%d, src:%s",g_connect_addr,convid,src)


	// network type detection
	l_serveraddr, err := net.ResolveUDPAddr("udp", g_connect_addr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	
	l_localaddr,err:=net.ResolveUDPAddr("udp",fmt.Sprintf("%s:%d",src,convid))
	if err != nil {
		l.Errorf("unable to bind to local src:%s error:",err)
		return nil, errors.WithStack(err)
	}

	network := "udp4"
	if l_serveraddr.IP.To4() == nil {
		network = "udp4"
	}
	l_localaddr=l_localaddr
	l_localaddr.Port=0 //choose random port

	l.Infof("connecting with network:%s to:%s",network,l_serveraddr)
	l_boundconn, err := net.DialUDP(network, l_localaddr,l_serveraddr)
	if err != nil {
		l.Errorf("DialUDP failed: %s",err)
		return nil,  errors.WithStack(err)
	}
	l.Infof("local addr:%s",l_localaddr)
	

	//create a RatSession, and specify the UDPConn bound to the src addr
	//It does mean we need to close it ourself later, as kcp won't do that since it doesn't own the UDPConn
	session,err:=NewClientSession(convid,l_localaddr,l_serveraddr,l_boundconn)
	return session,nil
}



func createTun(tunnelid uint32, ip string) (*water.Interface, error) {
	config := water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			MultiQueue:true,
			},
	}

	if g_tunnel_name!="" {
		config.Name=g_tunnel_name
	} else {
		config.Name = fmt.Sprintf("tun%d",tunnelid)
	}

	iface, err := water.New(config)
	if err != nil {
		return nil, err
	}
	l.Debugf("Interface Name: %s\n", iface.Name())
	runthing("ip","addr","add",ip+"/30","dev",iface.Name())
	runthing("ip","link","set",iface.Name(),"mtu",fmt.Sprintf("%d",(g_tunnel_mtu)))
	runthing("ip","link","set",iface.Name(),"up")
	return iface, nil
}


func client_handle_session(server *serverType, connection *serverConnection) {
	l.Infof("base_convid:%d, convid:%d",server.base_convid, connection.convid)
	for {
		message := make([]byte, 265535)
		for {
			if connection.session==nil {
				l.Errorf("session is empty, exiting thread")
				return
			}
			kcpstate:=connection.session.GetState()
			if kcpstate==0xFFFFFFFF {
				//the connection is dead according to kcp
				l.Errorf("kcp convid:%d is dead, closing...",connection.session.GetConv())

				client_disconnect_session_by_convid(server.base_convid,connection.convid,"RECV convid is dead")

				return
			}

			
			//2 seconds read deadline
			connection.session.SetReadDeadline(time.Now().Add(time.Millisecond*5000)) 

			n, err := connection.session.Read(message)
			if err != nil {
				if (fmt.Sprintf("%s",err)=="timeout") {
					l.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>read deadline exceeded: convid:%d",connection.convid)
					connection.rxtimeouts++
					l.Debugf("%s",printServerConnection(connection))
					if connection.rxtimeouts>g_client_max_rxtimeouts {
						client_disconnect_session_by_convid(server.base_convid,connection.convid,"rx timeouts exceeded")
						return
					}
					continue;
				}
				l.Debugf("conn read error:", err)
				client_disconnect_session_by_convid(server.base_convid,connection.convid,fmt.Sprintf("RECV conn read error:%s",err))

				return
			}

			
			//we can reset the local rxtimeouts if we received a packet.
			connection.rxtimeouts=0

			connection.rxcounter++
			connection.rxbytes+=uint64(n)
			//we can assume if we are receiving data it's like a hello
			connection.last_hello=time.Now()

			//check for hello message
			if n==len(g_server_hello) && (message[0]==0 && message[1]==0) {
				//this is an initial hello message, don't send it to the tun
				l.Infof("received HELLO convid:%d from:%s",connection.convid,connection.session.RemoteAddr());
				connection.last_hello=time.Now()
				continue;
			}

			//check for kcp mss probe ack message
			if (message[0]==0x02 && message[1]==0x00) {
				//this is a kcp mss probe ACK message, don't send it to the tun

				//decode the MSS that is being ack'ed
				b := make([]byte, 2)
				b[0]=message[2]
				b[1]=message[3]
				acked_mss:=int(binary.LittleEndian.Uint16(b))

				l.Infof("received MSS_PROBE_ACK:convid%d, acked_mss:%d",connection.convid,acked_mss);
				l.Infof("setting convid:%d mss:%d",connection.convid,acked_mss)				
				connection.mss=acked_mss
				connection.session.SetMss(connection.mss)
				connection.last_hello=time.Now()
				connection.probe_acks++
				connection.alive=true

				//try the next mss
				next_mss:=acked_mss+g_client_mss_increment
				if connection.mss==g_max_mss {
					l.Infof("reached maximum mss of %d",g_max_mss)
					//this should stop the probing
					connection.settled_mss=acked_mss
				} else if next_mss>g_max_mss {
					l.Errorf("breached maximum mss of %d",g_max_mss)
					//this should stop the probing
					connection.settled_mss=acked_mss				
				} else {
					connection.next_mss=next_mss
				}
				
				continue;
			}		

			if server.iface != nil {
				_, err = server.iface.Write(message[:n])
				if err != nil {
					l.Errorf("tun write err:", err)
				} else {
					l.Tracef("tun write done")
				}
			} else {
				l.Errorf("server.iface is nil")
			}
			if (g_trace) {
				l.Tracef("START - incoming packet (%d bytes) from KCP convid:%d",n,connection.session.GetConv())
				WritePacket(message[:n])
				l.Tracef("DONE - incoming packet from KCP")
			}
		}

	}
}


func client_send_hello(connection *serverConnection,base_convid uint32) {
	l.Infof("sending HELLO to server convid:%d, remote:%s",connection.convid,connection.session.RemoteAddr())

	_,err:=connection.session.SendHello()
	
	if err != nil {
		l.Errorf(">>>>>>>>>>>>>SEND HELLO error:%s convid:%d iface:%s",err,connection.convid,connection.ifname)
			connection.txtimeouts++
			client_disconnect_session_by_convid(base_convid,connection.convid,fmt.Sprintf(">>>>>>>>>>>>>>>HELLO deadline exceeded"))
	}
}


func client_send_mss_probe(connection *serverConnection,base_convid uint32) {
	l.Tracef("sending MSS_PROBE to server convid:%d",connection.convid)
	connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*6000)) 

	//if we've reached the maximum MSS, stop probing
	if connection.next_mss>g_max_mss {
		l.Infof("breached maximum mss of %d",g_max_mss)
		connection.settled_mss=connection.mss
		return		
	}

	//if we've settled on an MSS, stop probing, and apply the last known good mss
	if (connection.settled_mss!=0) {
		l.Infof("MSS is settled on %d, applying MSS:%d",connection.settled_mss,connection.settled_mss)
		connection.session.SetMss(connection.settled_mss)
		l.Infof("settled MTU:%d",connection.session.GetMtu())
		connection.mss_is_settled=true
		return
	}

	//early catch, if we missed 3 mss probe aks, we can assume further probing won't help
	missedacks:=connection.probe_count-connection.probe_acks
	if missedacks==3 && connection.settled_mss==0 {
		l.Infof("reached threshold of 3 missed MSS probe acks convid:%d",connection.convid)
		//settle the MSS on the prior successfull mss
		connection.settled_mss=connection.mss		
		return
	}
	if (connection.probe_count>=g_client_max_mss_probes) {
		l.Infof("reached maximum number of MSS probes")
		return
	}
	
	//allright, let's probe then...
	connection.probe_count++
	connection.session.SetMss(connection.next_mss)


	pkt_mss_probe := make([]byte, connection.next_mss) //create a buffer of exactly the probed mss size
	for i := range pkt_mss_probe { pkt_mss_probe[i] = 0xFF } //and fill it with a canary

	//create the message, fill in the mss being probed.
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(connection.next_mss))
	pkt_mss_probe[2]=b[0]
	pkt_mss_probe[3]=b[1]

	l.Infof("sending MSS_PROBE to server convid:%d, len:%d, probe_count:%d, missed acks:%d",connection.convid,len(pkt_mss_probe),connection.probe_count,missedacks)
	_, err:=connection.session.Write(pkt_mss_probe)
	if err != nil {
		if (fmt.Sprintf("%s",err)=="timeout") {
			kcpstate:=connection.session.GetState()
			l.Errorf(">>>>>>>>>>>>>MSS_PROBE write deadline exceeded: convid:%d kcpstate:%d iface:%s",connection.convid,kcpstate,connection.ifname)
			connection.txtimeouts++
		}
		l.Errorf("MSS_PROBE conn write error:", err)
	}
	
}

func client_probe_mss() {

	//iterate through the list of server connections
	for base_convid, server := range g_server_list { // Order not specified 
		l.Tracef("probing server:%d",base_convid)
		for convid,connection := range server.connections {

			//send the probe
			if connection.session!=nil {
				if !connection.mss_is_settled {
					go client_send_mss_probe(connection,server.base_convid)				
				}
			} else {
				l.Errorf("SESSION IS NIL error sending MSS_PROBE to server convid:%d",convid)
			}
		}	
	}
}



func client_send_server_pings() {

	//iterate through the list of server connections
	for base_convid, server := range g_server_list { // Order not specified 
		l.Tracef("pinging server:%d",base_convid)
		for convid,connection := range server.connections {

			//whilst we're in here, check the hello age
			t1 := time.Now()
			hellodiff := t1.Sub(connection.last_hello).Seconds()
			l.Tracef("convid:%d hello age:%.2f",convid,hellodiff)
			//if last hello >g_max_hello seconds kill the session
			if (hellodiff>g_max_hello) {
				client_disconnect_session_by_convid(server.base_convid,connection.convid,fmt.Sprintf("HELLO timeout age:%.2f",hellodiff))
				continue;
			}

			//send the hello			
			if connection.session!=nil {
				go client_send_hello(connection,server.base_convid)
			} else {
				l.Errorf("SESSION IS NIL error sending HELLO to server convid:%d",convid)
			}

			//calculate the bandwidth
			
			bwdiff := t1.Sub(connection.last_bw_update).Seconds()
			connection.txbandwidth=( float32(connection.txbytes) * (8 / 1000.0 / 1000.0) ) / float32(bwdiff)
			connection.txbytes=0
			connection.rxbandwidth=( float32(connection.rxbytes) * (8 / 1000.0 / 1000.0) ) / float32(bwdiff)
			connection.rxbytes=0
			connection.last_bw_update=t1			

		}
		
	}
}


func client_choose_conn(packet *[]byte, packet_len int, server *serverType) (uint32, error) {
	
	//basic round robin

	if packet!=nil && g_use_consistent_hashing {
		dst:=ExtractSrc(packet,packet_len)
		owner,err := server.consistent.Get(dst)
		if err!=nil {
			l.Errorf("could not choose connection owner:%s err:%s",owner,err)
			return 0,errors.New("consitent get failed")
		}
			
		u32, err := strconv.Atoi(owner)
		if (g_debug) {
			l.Debugf("consistent: dst=%s, owner=%d, consistent:%+v",dst,u32,server.consistent.Members())
		}
		connid:=uint32(u32)
		if !server.connections[connid].alive {
			return 0,errors.New("connection isn't alive")
		}
		return uint32(u32),err
	}
	

	if (len(server.connections)==0) {
		l.Errorf("no connections available: server.connections length=%d ",len(server.connections))
		return 0,errors.New("no connections available")
	} 
	var some_convid uint32=0
	for convid, conn := range server.connections { // Order not specified 
		//fmt.Println(key, value)
		if convid!=server.last_convid && conn.alive {
			server.last_convid=convid
			l.Tracef("chose convid:%d",convid)
			return convid,nil
		}
		some_convid=convid
	}
	//if all else fails, return some conversation id
	l.Tracef("default chose convid:%d",some_convid)
	server.last_convid=some_convid		
	return some_convid,nil
}

func client_handle_tun(server *serverType) {
	l.Infof("client_handle_tun:%d",server.base_convid)

	packet := make([]byte, 65535)
	if server.iface==nil {
		l.Errorf("cannot handle tun%d, iface not available",server.base_convid)
		return
	}
	//i:=0
	for {
		tryagain:

		n, err := server.iface.Read(packet)
		if err != nil {
			l.Errorf("tunnel iface read error:", err)
			return
		}

		l.Tracef("read: %d bytes from %s",n,server.iface.Name())
		//i++
		
		//l.Tracef("writing to conn1")

		//simplistic round robin
		
		chose_convid,err:=client_choose_conn(&packet,n,server)
		if err!=nil || chose_convid==0 {
			l.Errorf("no connection available to send to, waiting a bit... chose_convid:%d, err:%s",chose_convid,err)
			time.Sleep(1000 * time.Millisecond)

			//TODO: we should probably kick the netlink_subscribe_routes into action again to look for routes
			//or exit entirely and start afresh

			
			
			
			continue
		}
		server.mu.Lock()
		connection,ok:=server.connections[chose_convid] 
		server.mu.Unlock()
		if !ok {
			l.Errorf("connection for chose_convid:%d is not available",chose_convid);
			l.Debugf("g_server_list: \n%s",printServerList(g_server_list))
			continue
		}	
		
		if (connection.session==nil) {
			l.Errorf("no connection available to send to, waiting a bit")
			time.Sleep(100 * time.Millisecond)
			//TODO: we should probably kick the netlink_subscribe_routes into action again to look for routes
			//or exit entirely and start afresh
			continue
		}

		kcpstate:=connection.session.GetState()
		if kcpstate==0xFFFFFFFF {
			//the connection is dead according to kcp
			l.Errorf("kcp convid:%d is dead, closing...",connection.session.GetConv())
			client_disconnect_session_by_convid(server.base_convid,connection.convid,"WRITE convid is dead")

			//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
			goto tryagain;
		}

		//and send it out the selected KCP conn
		l.Tracef("chose_convid:%d sending via %s",chose_convid,connection.src_address)
		if err == nil {
			//l.Debugf("write kcp")

			connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*g_write_deadline)) 
			_, err = connection.session.Write(packet[:n])
			if err != nil {
				if (fmt.Sprintf("%s",err)=="timeout") {
					l.Debugf(">>>>>>>>>>>>>>>>>>>>>>>>>>>write deadline exceeded: convid:%d",connection.convid)
					connection.txtimeouts++
					continue;
				}
				l.Errorf("conn write error:", err)
				//close the server connection
				client_disconnect_session_by_convid(server.base_convid,connection.convid,"WRITE conn error")

				//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
				goto tryagain;
			}
			connection.txcounter++
			connection.txbytes+=uint64(n)
			
		}
	
		if (g_trace) {
			l.Tracef("START - incoming packet (%d bytes) from %s",n,server.iface.Name())
			WritePacket(packet[:n])
			l.Tracef("DONE - incoming packet from %s",server.iface.Name())
		}

	}
}


func server_listen_udp(tunnelid uint32) {

	g_remoteConns = new(sync.Map)
	
	for {
		buf := make([]byte, 1500)
		n, remoteaddr, err := g_listener.ReadFromUDP(buf)
		
		if err != nil {
			continue
		}

  		if s, ok := g_remoteConns.Load(remoteaddr.String()); !ok {
			
			//this is a new connection
   			//check for hello message

			g_remoteConns.Range(func(key, value interface{}) bool {
				l.Infof("remoteConn:%s",key.(string))
				return true
			})


			l.Infof("pkt from NEWCONN:%s, data:%x",remoteaddr,buf[:n])

			if n==len(g_client_hello) && (buf[0]==0 && buf[1]==0) {

				//TODO: encrypt control messages using...
				// https://pkg.go.dev/golang.org/x/crypto@v0.27.0/nacl/secretbox

				//decode the convid in the hello
				b := make([]byte, 2)
				b[0]=buf[2]
				b[1]=buf[3]
				hello_convid:=uint32(binary.LittleEndian.Uint16(b))

				localaddr:=g_listener.LocalAddr().(*net.UDPAddr)

				l.Infof("received INITIAL HELLO from %s, convid:%d",remoteaddr,hello_convid)
				if (hello_convid==0) {
					l.Errorf("received invalid convid:%d from %s", hello_convid, remoteaddr)
					continue
				}

				session,_:=NewServerSession(hello_convid,localaddr,remoteaddr,g_listener)
				g_remoteConns.Store(remoteaddr.String(), session)

				g_remoteConns.Range(func(key, value interface{}) bool {
					l.Infof("remoteConn:%s",key.(string))
					return true
				})

				go server_accept_conn(tunnelid,hello_convid,session)
			} else {
				l.Errorf("received malformed hello from %s, data:%x",remoteaddr,buf[:n])
				continue
			}
			
  		} else {
			//this is an existing connection, send it to it's data notification channel
			//cast it to a RatSession
			session:=s.(*RatSession)
			l.Tracef("pkt from EXISTING:%s, %+v, data:%x",remoteaddr,session,buf[:n])
			session.DataReceived(buf[:n])
		}		
	}
}

func ExtractDst(frame *[]byte,frame_len int) string {
	
	header, err := ipv4.ParseHeader(*frame)
	
	
	if err != nil {
		l.Errorf("ExtractDst packet err:", err)
	} else {
		//l.Tracef("ExtractDst:SRC:%s", header.Src)
		//l.Tracef("ExtractDst:DST:%s", header.Dst)
	}
	
	//is it a tcp packet, and enough size?
	if (header.Protocol==6 && frame_len>=24) {
		l.Debugf("TCP packet!")		
		//src port
		//l.Debugf("%#x",(*frame)[20])
		//l.Debugf("%#x",(*frame)[21])
		//dst port
		//l.Debugf("%#x",(*frame)[22])
		//l.Debugf("%#x",(*frame)[23])
		dstport:=fmt.Sprintf("%x%x:%x%x:%s:%s",(*frame)[22],(*frame)[23],(*frame)[20],(*frame)[21],header.Src,header.Dst)
		//l.Debugf("dstport:%s",dstport)
		return dstport

		packet,err:=parsetcp.ParseTCPPacket(*frame)
		if err!=nil {
			l.Errorf("could not parse tcp packet")
		} else {
			
			//l.Tracef("tcp dstip:%s dstport:%s",packet.IP.DstIP,packet.TCP.DstPort)
			return fmt.Sprintf("%s:%s",packet.IP.DstIP,packet.TCP.DstPort)
		}
	}	
	//l.Tracef("other dst:%s",header.Dst)
	return fmt.Sprintf("%s:%s",header.Src,header.Dst)
}

func ExtractSrc(frame *[]byte, frame_len int) string {

	
	header, err := ipv4.ParseHeader(*frame)
	
	if err != nil {
		l.Errorf("ExtractSrc packet err:", err)
	} else {
		//l.Tracef("ExtractSrc:SRC:%s", header.Src)
		//l.Tracef("Extractsrc:DST:%s", header.Dst)
	}

	

	//is it a tcp packet
	if (header.Protocol==6 && frame_len>=24) {
		//l.Tracef("TCP packet!")		
		//src port
		//l.Debugf("%#x",(*frame)[20])
		//l.Debugf("%#x",(*frame)[21])
		//dst port
		//l.Debugf("%#x",(*frame)[22])
		//l.Debugf("%#x",(*frame)[23])
		//dstport:=fmt.Sprintf("%x%x",(*frame)[20],(*frame)[21])
		dstport:=fmt.Sprintf("%x%x:%x%x:%s:%s",(*frame)[20],(*frame)[21],(*frame)[22],(*frame)[23],header.Dst,header.Src)
		//l.Debugf("srcport:%s",dstport)
		return dstport
		packet,err:=parsetcp.ParseTCPPacket(*frame)
		if err!=nil {
			l.Errorf("could not parse tcp packet")
		} else {
			//l.Tracef("tcp dstip:%s dstport:%s",packet.IP.SrcIP,packet.TCP.SrcPort)
			return fmt.Sprintf("%s:%s",packet.IP.SrcIP,packet.TCP.SrcPort)
		}
	}	
	//l.Tracef("other src:%s",header.Src)
	return fmt.Sprintf("%s:%s",header.Dst,header.Src)
}


func server_accept_conn(tunnelid uint32, convid uint32, session *RatSession) {

	
	l.Infof("connection accepted convid:%d, remote:%s",convid ,session.RemoteAddr())



	//find/add the client in the list
	client,ok:=g_client_list[tunnelid]
	if !ok {
		//new client
		client=new(clientType)
		client.mu=new(sync.Mutex)
	}
	g_client_list[tunnelid]=client

	
	client.mu.Lock()
	
	//if there's no interface, create the tun for them
	spawn_tun:=false
	if (client.iface == nil) {		
		iface,err:=createTun(tunnelid,g_server_tun_ip)
		if err!=nil {
			l.Errorf("error creating tun%d interface :%s",tunnelid,err)
		}
		client.iface=iface
		client.my_tun_ip=g_server_tun_ip
		client.remote_tun_ip=g_client_tun_ip
		client.ifname=iface.Name()
		client.base_convid=tunnelid	

		spawn_tun=true
	}

	
	
	client.last_convid=0 //reset the scheduler

	//this is all a bit icky hacky
	var connection=new(clientConnection)
	connection.session=session
	connection.session.convid=session.GetConv()

	connection.txbandwidth=0; 
	connection.rxbandwidth=0; 
	connection.last_bw_update=time.Now()
	connection.rxloss=0; connection.txloss=0; connection.alive=true; 
	connection.last_hello=time.Now()
	connection.up_since=time.Now()
	connection.txcounter=0
	connection.txbytes=0
	connection.rxcounter=0
	connection.rxbytes=0
	connection.rxtimeouts=0
	connection.txtimeouts=0
	connection.priority=0
	connection.mss=g_server_mss
	connection.session.SetMss(connection.mss)

		
	connection.convid=convid
	connection.src_address=fmt.Sprintf("%s",session.RemoteAddr())
	ip:=net.ParseIP(connection.src_address)
	if ip.IsPrivate() {
		l.Errorf("!!!!!!!!!!!!!!!!!cannot accept connection with private ip address: %s",session.RemoteAddr())
		connection=nil
		client.mu.Unlock()
		return
	}

	//create the map if required
	if client.connections==nil {
		client.connections=make(map[uint32]*clientConnection)
		client.consistent = consistent.New()
	}
	

	client.connections[session.GetConv()]=connection

	client.consistent.Add( fmt.Sprintf("%d",session.GetConv()) )
	

	//update the main list
	g_client_list[tunnelid]=client


	client.mu.Unlock()

	//l.Warnf("g_client_list: %+v",g_client_list)
	l.Debugf("after accept: g_client_list: \n%s",printClientList(g_client_list))


	
	//spawn the thread that handles the tunnel
	if spawn_tun {
		go server_handle_tun(client)
	}
	
	//and read forever on the connection
	for {
		
		message := make([]byte, 65535)

		//read forever on the KCP connection, and send it to the tun dev
		for {

			if session==nil {
				l.Errorf("session is nil, exiting thread")
				return
			}

			kcpstate:=session.GetState()
			if kcpstate==0xFFFFFFFF {
				//the connection is dead according to kcp
				l.Errorf("kcp convid:%d is DEAD, closing...",convid)
				server_disconnect_session_by_convid(client.base_convid,convid, "KCP Conn Dead",session)
				return
			}

			if (session.is_closed) {
				//the connection is dead according to kcp
				l.Errorf("session:%s is closed, exiting...",session.remote_addr.String())
				return
			}

			//2 seconds read deadline
			session.SetReadDeadline(time.Now().Add(time.Millisecond*5000)) 
			
			n, err := session.Read(message)
			
			if err != nil {
				if (fmt.Sprintf("%s",err)=="timeout") {
					l.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>server read deadline exceeded: convid:%d, kcp state:%d, n:%d, remoteaddr:%s",convid,kcpstate,n,connection.session.RemoteAddr())
					connection.rxtimeouts++
					if connection.rxtimeouts>g_client_max_rxtimeouts {
						server_disconnect_session_by_convid(client.base_convid,convid, "rx timeouts exceeded",connection.session)
						return
					}
					continue;
				}
				l.Debugf("conn read error:%s", err)
				//close the client connection
				server_disconnect_session_by_convid(tunnelid,convid,fmt.Sprintf("Server Conn Read Error:%s",err),connection.session)
				return
			}

			if (n<=0) {
				l.Errorf("Read <=0 (read:%d)",n)
				continue
			}

			l.Tracef("received pkt from:%s len:%d, data:%x",session.RemoteAddr(),n,message[:n])

			//we can reset the local rxtimeouts if we received a packet.
			connection.rxtimeouts=0
			connection.rxcounter++
			connection.rxbytes+=uint64(n)
			//we can assume if we are receiving data it's like a hello
			connection.last_hello=time.Now()

			//check for hello message
			if n==len(g_client_hello) && (message[0]==0 && message[1]==0) {
				
				//decode the convid in the hello
				b := make([]byte, 2)
				b[0]=message[2]
				b[1]=message[3]
				hello_convid:=binary.LittleEndian.Uint16(b)
				//this is an initial hello message, don't send it to the tun
				l.Infof("received HELLO convid:%d from:%s",hello_convid,connection.session.RemoteAddr());
				connection.last_hello=time.Now()
				continue;
			}

			//check for mss probe message (which we should be able to read in one foul swoop, at exactly the correct size)
			//probe messages are 0xFF filled for the entire message
			if (message[0]==0xFF && message[1]==0xFF) {
				//this is an mss probe message, don't send it to the tun
				//decode the mss that is being probed
				b := make([]byte, 2)
				b[0]=message[2]
				b[1]=message[3]
				probed_mss:=int(binary.LittleEndian.Uint16(b))
				l.Infof("received MSS_PROBE convid:%d, len:%d, probed_mss:%d",convid,n,probed_mss);
				connection.last_hello=time.Now()
				connection.alive=true

				//if the probed MSS length matches the received packet size, then we can acknowledge it
				if probed_mss==n {
					l.Infof("MSS_PROBE data len:%d matched probed mss:%d, sending MSS_PROBE_ACK",n,probed_mss)					
					l.Infof("setting convid:%d mss:%d",convid,probed_mss)
					connection.mss=probed_mss
					connection.session.SetMss(connection.mss)
					go server_send_probe_ack(connection,probed_mss)
				} else {
					l.Warnf("received incorrectly sized probe message, len:%d allegedly probed mss:%d",n,probed_mss);
				}
				continue;
			}

			//check for linkdown message, exactly 3 bytes starting with a zero
			if n==3 && (message[0]==0) {
				//this is a linkdown message, don't send it to the tun
				l.Debugf("linkdown msg len:%d",len(message))
				//decode the convid that is down
				b := make([]byte, 2)
				b[0]=message[1]
				b[1]=message[2]
				linkdown:=binary.LittleEndian.Uint16(b)

				l.Errorf("received LINKDOWN:convid%d",linkdown);

				//close the client connection
				server_disconnect_session_by_convid(tunnelid,uint32(linkdown),"RECVD Linkdown",connection.session)
				continue;
			}

			

			//check for invalid packet, probably leftover part of an oversize probe
			if (message[0]==0 && message[1]==0) {
				l.Warnf("stray leftover probe packet? len: %d, data:%x", n,message[:n])
				continue;
			}

			//write the packet to the tun
			if client.iface != nil {
				_, err = client.iface.Write(message[:n])
				if err != nil {
					l.Errorf("iface write err:%s, len:%d", err,n)
					//close the client connection
					server_disconnect_session_by_convid(tunnelid,convid,"TUN write error",connection.session)
					return
				} else {
					l.Tracef("iface write done")
				}
			}
			if (g_trace) {
				l.Tracef("START - incoming packet (%d bytes) from KCP convid:%d",n,connection.session.GetConv())
				WritePacket(message[:n])
				l.Tracef("DONE - incoming packet from KCP")
			}
		}

	}
}


func server_close_connection(client *clientType, client_conn *clientConnection, convid uint32) {
	client.last_convid=0 //restart the round robin
	client_conn.session.Close()
}

func server_disconnect_session_by_convid(tunnelid uint32, disc_convid uint32,reason string, session *RatSession) {
	l.Warnf("server disconnect tunnelid:%d, convid:%d reason:%s",tunnelid,disc_convid,reason)
	client,ok := g_client_list[tunnelid]
	if !ok || client==nil {
		l.Errorf("cannot disconnect tunnelid:%d convid:%d- no such client connection/tunnel",tunnelid,disc_convid)
		
		return
	}

	if session!=nil {
		session.Close()
	}

	//find the kcp connection matching the disc_convid and disconnect/destroy it
	client.mu.Lock()
	for convid, client_connection := range client.connections {
		if client_connection.convid==disc_convid {
			if client_connection.session.is_closed {
				l.Errorf("session is already closed!")
				continue
			}
			l.Infof("disconnecting tunnelid:%d, kcp convid: %d",tunnelid,client_connection.convid)
			client.consistent.Remove(fmt.Sprintf("%d",convid)) //remove it from consistent hashing
			server_close_connection(client,client_connection,convid)			
			delete(client.connections,convid)			
		}
	}	
	client.mu.Unlock()

	l.Debugf("AFTER DELETE: g_client_list: \n%s",printClientList(g_client_list))
}


func server_send_hello(connection *clientConnection) {
	l.Infof("sending HELLO to client convid:%d, remote:%s",connection.convid,connection.session.RemoteAddr())
	connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*3000)) 
	n, err:=connection.session.Write(g_server_hello)
	if (n<=0) {
		l.Errorf("err: %d whilst sending hello", n )
		return
	}
	if err != nil {
		if (fmt.Sprintf("%s",err)=="timeout") {
			kcpstate:=connection.session.GetState()
			l.Warnf("HELLO write deadline exceeded: convid:%d kcpstate:%d, ifname:%s",connection.convid,kcpstate)
			connection.txtimeouts++
			return;
		}
		l.Errorf("HELLO conn write error:", err)
	}
}

func server_send_probe_ack(connection *clientConnection, acked_len int) {
	l.Infof("sending MSS_PROBE_ACK to client convid:%d",connection.convid)
	connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*3000)) 

	kcp_mss_probe_ack := make([]byte, 4)
	kcp_mss_probe_ack[0]=0x02
	kcp_mss_probe_ack[1]=0x00

	//create the message
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(acked_len))
	kcp_mss_probe_ack[2]=b[0]
	kcp_mss_probe_ack[3]=b[1]

	l.Infof("sending MSS_PROBE_ACK to client convid:%d, len:%d",connection.convid,len(kcp_mss_probe_ack))
	_, err:=connection.session.Write(kcp_mss_probe_ack)
	if err != nil {
		if (fmt.Sprintf("%s",err)=="timeout") {
			kcpstate:=connection.session.GetState()
			l.Errorf(">>>>>>>>>>>>>MSS_PROBE_ACK write deadline exceeded: convid:%d kcpstate:%d",connection.convid,kcpstate)
			connection.txtimeouts++
		}
		l.Errorf("MSS_PROBE_ACK conn write error:", err)
	}
}

func server_send_client_pings() {

	//iterate through the list of clients
	for base_convid, client := range g_client_list { // Order not specified 
		l.Tracef("pinging client:%d",base_convid)
		for convid,connection := range client.connections {

			//whilst we're in there, check the hello age	
			t1 := time.Now()		
			diff := t1.Sub(connection.last_hello).Seconds()
			l.Tracef("convid:%d hello age:%.2f",convid,diff)
			//if last hello >g_max_hello  kill the session
			if (diff>g_max_hello) {
				server_disconnect_session_by_convid(client.base_convid,connection.convid,fmt.Sprintf("HELLO timeout age:%.2f",diff),nil)
				continue;
			}	

			//send the hello
			if connection.session!=nil {
				go server_send_hello(connection)
			} else {
				l.Errorf("SESSION IS NIL error sending HELLO to server convid:%d",convid)
			}

			//calculate the bandwidth			
			bwdiff := t1.Sub(connection.last_bw_update).Seconds()
			connection.txbandwidth=( float32(connection.txbytes) * (8 / 1000.0 / 1000.0) ) / float32(bwdiff)
			connection.txbytes=0
			connection.rxbandwidth=( float32(connection.rxbytes) * (8 / 1000.0 / 1000.0) ) / float32(bwdiff)
			connection.rxbytes=0
			connection.last_bw_update=t1

					
		}
		
	}
}

func server_choose_conn(packet *[]byte,packet_len int,client *clientType) (uint32, error) {
	//lock the structs
	//l.Warnf("client: %+v",client)
	//basic round robin

	if packet!=nil && g_use_consistent_hashing {
		dst:=ExtractDst(packet,packet_len)
		owner,err := client.consistent.Get(dst)
		if err!=nil {
			return 0,err
		}
			

		u32, err := strconv.Atoi(owner)
		if (g_debug) {
			l.Debugf("consistent: dst=%s, owner=%d, consistent:%+v",dst,u32,client.consistent.Members())
		}

		connid:=uint32(u32)
		if !client.connections[connid].alive {
			return 0,errors.New("connection isn't alive")
		}
		return connid,err
	}
	

	var some_convid uint32=0
	for convid, client_connection := range client.connections { // Order not specified 
		//fmt.Println(key, value)
		if convid!=client.last_convid && client_connection.alive {
			client.last_convid=convid
			//l.Tracef("chose convid:%d",convid)
			return convid,nil
		}
		some_convid=convid
	}
	//if all else fails, return some or the other convid
	//l.Tracef("default chose convid:%d",some_convid)
	client.last_convid=some_convid
	return some_convid,nil
}

func server_handle_tun(client *clientType) {	
	l.Infof("starting tunnel thread............")
	if (client.iface==nil) {
		l.Errorf("cannot handle nonexisting tunnel iface: %d",client.base_convid)
		return
	}

	l.Infof("server_handle_tun:%s",client.iface.Name())
	packet := make([]byte, 65535)

	for {

		tryagain:

		//read the packet from the wire
		//l.Debugf("read tun")
		l.Tracef("Server about to read from tun:")
		n, err := client.iface.Read(packet)		
		l.Tracef("Server finished read from tun:")
		if err != nil {
			l.Panicf("tun iface read error:", err)
			//close the client connection
			//TODO
			return
		}

		
		//find a kcp connection for the packet
		//cheat, but in future balance accross all conns
		
		chose_convid,err:=server_choose_conn(&packet,n,client)
		if err!=nil {
			l.Errorf("no conversations available to send with convid:%d, %s",chose_convid,err);
			time.Sleep(10 * time.Millisecond)
			continue;
		}
		client.mu.Lock()
		connection,ok:=client.connections[chose_convid] 
		client.mu.Unlock()
		if !ok {
			l.Errorf("connection for chose_convid:%d is not available",chose_convid);
			l.Debugf("g_server_list: \n%s",printServerList(g_server_list))
			time.Sleep(10 * time.Millisecond)
			continue
		}	
		l.Tracef("chose_convid:%d",chose_convid)
		if (connection.session==nil) {
			l.Errorf("no connection available to send to, waiting a bit")
			time.Sleep(10 * time.Millisecond)
			continue
		}

		kcpstate:=connection.session.GetState()
		if kcpstate==0xFFFFFFFF {
			//this connection is dead according to state layer
			l.Errorf("convid:%d is DEAD, closing...",connection.convid)
			server_disconnect_session_by_convid(client.base_convid,connection.convid,"KCP dead",nil)

			//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
			goto tryagain;
		}

		
		//and send it out the selected KCP conn
		if err == nil {
			l.Tracef("server write kcp: convid%d",connection.convid)
			
			connection.session.SetWriteDeadline(time.Now().Add(time.Millisecond*g_write_deadline))
			_, err = connection.session.Write(packet[:n])
			l.Tracef("server finished write kcp: convid%d",connection.convid)
			if err != nil {
				if (fmt.Sprintf("%s",err)=="timeout") {
					l.Infof(">>>>>>>>>>>>>>>>>>>>>>>>>>>write deadline exceeded: convid:%d",connection.convid)
					connection.txtimeouts++
					l.Debugf("%s",printClientConnection(connection))
					continue;
				}
				l.Errorf("kcp conn write error:", err)
				//close the client connection
				server_disconnect_session_by_convid(client.base_convid,connection.convid,"KCP Conn write error",nil)

				//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
				goto tryagain;
			}
			connection.txcounter++	
			connection.txbytes+=uint64(n)
		}


		if (g_trace) {
			l.Tracef("START - incoming packet (%d bytes) from %s",n,client.iface.Name())
			WritePacket(packet[:n])
			l.Tracef("DONE - incoming packet from %s",client.iface.Name())
		}

	}
}



func WritePacket(frame []byte) {
	header, err := ipv4.ParseHeader(frame)
	
	if err != nil {
		l.Tracef("write packet err:", err)
	} else {
		l.Tracef("SRC:%s", header.Src)
		l.Tracef("DST:%s", header.Dst)
	}
}

// ----------------------------------------------------
// MAIN -- parse args and dispatch
// ----------------------------------------------------

var CLI struct {
	Debug     bool `help:"Enable debug."`
	Trace     bool `help:"Enable tracing."`
	Nosyslog    bool `help:"Disable syslog. (default is to use syslog)"`
	Secret    string `help:"Secret AES key. (default:ratbond)"`
	Aes       bool `help:"Enable AES encryption."`
	BalanceConsistent bool `help:"Use consistent hashing packet scheduler (default)" default:"1"`
	BalanceRoundrobin bool `help:"Use round robin packet scheduler."`
	ConnectAddr string `help:"connect to server address:port (default:154.0.6.97:12345)"`
	ListenAddr   string `help:"bind server to listen on address:port (default:0.0.0.0:12345)"`
	MqttBrokerAddr   string `help:"mqtt broker to connect to address:port (default:things.byteheavy.com:1833)"`
	MqttUsername   string `help:"mqtt broker username: default nil"`
	MqttPassword   string `help:"mqtt broker password: default nil"`
	TunnelId      uint32 `help:"required: tunnel-id to use between client and server, has to match on both sides (default:nil)" default:"0"`
	TunName      string `help:"name of the tun interface on the client (e.g. tun0, tun1), defaults to tun<tunnel-id>"`
	TunnelIp     string `help:"/30 (point to point) IP address to assign on client/server tun interfaces. Has to match on both sides. (default:10.10.10.0/30)"`
	Mux      uint32 `help:"multiplex (n) number of KCP session on an interface (default:1)" default:"1"`
	HttpListenAddr  string `help:"bind status httpserver to listen on address:port (default:0.0.0.0:8091), set to http-listen-addr=disable to not service http requests"`
	
	Client struct {
		
	} `cmd:"" help:"Act as bond client (default)." default:"1"`

	Server struct {
	} `cmd:"" help:"As a bond server."` 

	Connectaggregator struct {
		} `cmd:"" help:"Connect to a bonding aggregation server, using MQTT to manage clients."` 
}


func parse_cli() string {
	ctx := kong.Parse(&CLI)

	if (CLI.Debug) {
		g_debug = true
		
	}
	if (CLI.Trace) {
		g_trace = true
		g_debug = true

	}
	if (CLI.Nosyslog) {
		g_use_syslog = false
	}

	init_logging()

	l.Infof("ratbond version:%s, revision:%s",versioninfo.Version, versioninfo.Revision)

	if (g_trace) {
		l.Infof("trace enabled")
	}
	if (g_debug) {
		l.Infof("debug enabled")
	}

	if (CLI.BalanceConsistent) {
		g_use_consistent_hashing = true
		l.Infof("enabling balance-consistent")
	}
	if (CLI.BalanceRoundrobin) {
		g_use_consistent_hashing = false
		l.Infof("enabling balance-roundrobin")
	}

	if (CLI.Secret!="") {
		g_secret=CLI.Secret
		l.Infof("using secret:%s","<hidden>")
	}

	if (CLI.Mux!=1) {
		g_mux_max=int(CLI.Mux)
	}
	l.Infof("max-mux: %d", g_mux_max)

	if (CLI.Aes) {
		g_use_aes = true
		l.Infof("enabling AES encryption")
	}

	if (CLI.TunnelId!=0) {
		g_tunnel_id=uint32(CLI.TunnelId)
	}
	
	l.Infof("tunnel-id=%d",g_tunnel_id)

	if (CLI.ConnectAddr!="") {
		g_connect_addr = CLI.ConnectAddr		
		ipport,err:=netip.ParseAddrPort(g_connect_addr)
		g_connect_ip=fmt.Sprintf("%s",ipport.Addr())
		if (err!=nil) {
			l.Errorf("connect-addr: %s error: %s",CLI.ConnectAddr,err)
			os.Exit(1)
		}
	}
	if (CLI.ListenAddr!="") {
		g_listen_addr = CLI.ListenAddr		
		_,err:=netip.ParseAddrPort(g_listen_addr)
		if (err!=nil) {
			l.Errorf("listen-addr: %s error: %s",CLI.ListenAddr,err)
			os.Exit(1)
		}
	}

	if (CLI.MqttBrokerAddr!="") {
		g_mqtt_broker_addr = CLI.MqttBrokerAddr		
		_,err:=netip.ParseAddrPort(g_mqtt_broker_addr)
		if (err!=nil) {
			l.Errorf("mqtt-broker-addr: %s error: %s",CLI.MqttBrokerAddr,err)
			os.Exit(1)
		}
	}

	if (CLI.MqttUsername!="") {
		g_mqtt_username = CLI.MqttUsername		
	}
	if (CLI.MqttPassword!="") {
		g_mqtt_password = CLI.MqttPassword		
	}
	
	if (CLI.HttpListenAddr!="") {
		if CLI.HttpListenAddr=="disable" {
			g_http_listen_addr=""
		} else {
			g_http_listen_addr = CLI.HttpListenAddr
			_,err:=netip.ParseAddrPort(g_http_listen_addr)
			if (err!=nil) {
				l.Errorf("http-listen-addr: %s error: %s",CLI.HttpListenAddr,err)
				os.Exit(1)
			}
		}
	}
	if (CLI.TunnelIp!="") {
		l.Infof("tunnel-ip: parsing %s",CLI.TunnelIp)
		_, ipnet, err := net.ParseCIDR(CLI.TunnelIp)
		if err != nil {
			l.Errorf("error:%s",err)
			os.Exit(1)
		}
		if (fmt.Sprintf("%s",ipnet.Mask)!="fffffffc") {
			l.Errorf("%s is not a /30 CIDR",CLI.TunnelIp)
			os.Exit(1)
		}

		hosts,err:=IpHosts(CLI.TunnelIp)
		if err!=nil || len(hosts)<2 {
			l.Errorf("not enough hosts in subnet %s to use for point-to-point",CLI.TunnelIp)
			os.Exit(1)
		}
		//use first address for client
		g_client_tun_ip=hosts[0]		
		//second address on the server
		g_server_tun_ip=hosts[1]				
	}
	l.Infof("tunnel-ip: using %s on client",g_client_tun_ip)
	l.Infof("tunnel-ip: using %s on server",g_server_tun_ip)	

	return ctx.Command()
}

func main() {

	command:=parse_cli()

	network_sysctls()

	switch command {
		case "client" :	{ 
			run_client()
		}
		case "server" : {			
			run_server()
		}
		case "connectaggregator" : {			
			run_aggregator()
		}
		default: {
			l.Errorf("unknown command:%s",command)
			os.Exit(1)
		}
	}

}
