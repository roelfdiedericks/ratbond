package main

import (
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"


	"github.com/alecthomas/kong"

	"github.com/songgao/water"

	"golang.org/x/net/ipv4"

	"crypto/sha1"

	"github.com/roelfdiedericks/kcp-go"

	"golang.org/x/crypto/pbkdf2"
	"github.com/prometheus-community/pro-bing"
)

var g_client bool = false
var g_debug bool = true
var g_trace bool = false
var g_server_addr = "154.0.6.97:12345"
var g_listen_addr = "0.0.0.0:12345"
var g_buffer_size = 16777217
var g_client_tunnelid uint32=660
var g_server_tun_ip string ="10.10.10.2"
var g_client_tun_ip string ="10.10.10.1"

var g_tunnel_mtu uint32=1350
var g_reorder_buffer_size=128


var g_data string

var g_listener *kcp.Listener

type packetType struct {
	sequence uint64
	len int
	buff []byte
}



/* ----------------------------------------------------*/
/* structs for the server, dealing with clients conns  */
/* ----------------------------------------------------*/
type client_kcp struct {
	convid uint32
	kcp *kcp.UDPSession
	writer_channel chan packetType
	rxloss uint32
	txloss uint32
	txcounter uint64
	rxcounter uint64
	priority uint32
	bandwidth uint32
	
	last_hello time.Time
	alive bool
	src_address string
}

type clientType struct {
	client_kcps map[uint32] *client_kcp
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	tun_ip string
	reorder_buffer []packetType
}

var g_client_list =make(map[uint32] *clientType)




/* --------------------------------------------------*/
/* structs for the client, dealing with serverconns  */
/* --------------------------------------------------*/
type server_kcp struct {
	convid uint32
	kcp *kcp.UDPSession
	udp_conn *net.UDPConn
	writer_channel chan packetType
	ifname string
	rxloss uint32
	txloss uint32
	txcounter uint64
	rxcounter uint64
	priority uint32
	bandwidth uint32

	last_hello time.Time
	alive bool
	src_address string
}

type serverType struct {
	server_kcps map[uint32] *server_kcp
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	tun_ip string

	reorder_buffer []packetType
}

var g_server_list =make(map[uint32] *serverType)





func client_connection_by_src_exists(src string,server *serverType) (bool) {
	for _ , server_kcp := range server.server_kcps {
		if server_kcp.src_address==src {
			return true
		}
	}	
	return false
}


func client_connect_server(tunnelid uint32, src string, ifname string, gw string) {
	l.Tracef("================================================\nconnecting to server, base tunnelid:%d, src:%s, ifname:%s gw:%s",tunnelid,src,ifname,gw)
	

	server,ok:=g_server_list[tunnelid]
	if !ok {
		server=new(serverType)
		g_server_list[tunnelid]=server
	}
		

	//if there's no interface, create the tun for them
	spawn_tun:=false
	if (server.iface == nil) {		
		iface,err:=createTun(g_client_tun_ip,tunnelid)
		if err!=nil {
			l.Errorf("error creating tun%d interface :%s",tunnelid,err)
		}
		server.iface=iface
		server.tun_ip=g_client_tun_ip
		server.ifname=fmt.Sprintf("tun%d",tunnelid)
		server.base_convid=tunnelid	

		spawn_tun=true
	}



	//create the map if required
	if server.server_kcps==nil {
		server.server_kcps=make(map[uint32] *server_kcp)
	}

	//create the reorder buffer if required
	if (server.reorder_buffer==nil) {
		
		//server.reorder_buffer=make([]packet,g_reorder_buffer_size)
		//l.Warnf("created reorder buffer size:%d",g_reorder_buffer_size)
	}

	//see if the connection already exists
	if client_connection_by_src_exists(src,server) {
		l.Infof("already have a connection with src:%s",src);
		return
	}


	l.Infof("================================================\nconnecting to server, base tunnelid:%d, src:%s, ifname:%s gw:%s",tunnelid,src,ifname,gw)

	//find an open slot 0-9 on the base tunnelid, and start a connection
	var avail uint32
	for avail=tunnelid; avail<=tunnelid+10; avail++ {
		_, ok := server.server_kcps[avail]
		if !ok {
			//we can use this convid
			break;
		}
	}
	if avail==tunnelid+10 {
		l.Errorf("no more additional connections available for tunnel: %d",tunnelid)
		return
	}
	l.Debugf("found available convid:%d",avail)
	server.last_convid=0 //reset the scheduler

	

	kcp, udp_conn, err := create_udp_conn(avail,src)
	if err != nil {
		l.Panicf("kcp conn create error:", err)
	}

	//add the new connection the the list
	var server_kcp_con=new(server_kcp)
	server.server_kcps[avail]=server_kcp_con

	//init the struct
	server_kcp_con.bandwidth=10; server_kcp_con.rxloss=0; server_kcp_con.txloss=0; server_kcp_con.alive=true; //server_kcp_con.last_hello=0
	server_kcp_con.txcounter=1
	server_kcp_con.rxcounter=1
	server_kcp_con.priority=0
	server_kcp_con.kcp=kcp
	server_kcp_con.udp_conn=udp_conn
	server_kcp_con.convid=avail
	server_kcp_con.src_address=src
	server_kcp_con.ifname=ifname
	
	//create a channel for the kcp channel writer, with a 10 packet buffer
	server_kcp_con.writer_channel=make(chan packetType,100)
	go client_kcp_writer(server,server_kcp_con,server_kcp_con.writer_channel)

	g_server_list[tunnelid]=server
	l.Warnf("g_server_list: \n%s",printServerList(g_server_list))
	//l.Warnf("g_server_list: \n%+v",g_server_list)

	if err := kcp.SetReadBuffer(g_buffer_size); err != nil {
		l.Errorf("SetReadBuffer:", err)
	}
	if err := kcp.SetWriteBuffer(g_buffer_size); err != nil {
		l.Errorf("SetWriteBuffer:", err)
	}
	setClientConnOptions(kcp)

	//write something to the KCP to wake the other end
	hello := []byte("\x00\x00")
	kcp.SetWriteDeadline(time.Now().Add(time.Millisecond*10)) 
	kcp.Write(hello)
	go client_handle_kcp(server,server_kcp_con)
	if (spawn_tun) {
		go client_handle_tun(server)
	}



}

func client_disconnect_session_by_ifname(tunnelid uint32, ifname string) {
	l.Warnf("INTERFACE %s DOWN! DISCONNECTING from server, base tunnelid:%d, ifname:%s",ifname,tunnelid,ifname)
	
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
		return
	}

	//find the kcp connection matching the ifname and disconnect/destroy it
	for convid, server_kcp := range server.server_kcps {
		if server_kcp.ifname==ifname {
			l.Infof("DISCONNECTING kcp convid: %d",server_kcp.convid)
			server_kcp.kcp.Close()
			server_kcp.udp_conn.Close()
			server.last_convid=0 //restart the round robin
			delete(server.server_kcps,convid)
		}
	}	
}

func client_disconnect_session_by_src(tunnelid uint32, src string, ifname string, gw string) {
	l.Warnf("DISCONNECTING from server, base tunnelid:%d, src:%s, ifname:%s gw:%s",tunnelid,src,ifname,gw)
	
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
	}

	//find the kcp connection matching the src address and disconnect/destroy it
	for convid, server_kcp := range server.server_kcps {
		if server_kcp.src_address==src {
			l.Infof("disconnecting kcp convid: %d",server_kcp.convid)
			server_kcp.kcp.Close()
			server_kcp.udp_conn.Close()
			server.last_convid=0 //restart the round robin
			delete(server.server_kcps,convid)
		}
	}	
}

func client_disconnect_session_by_convid(tunnelid uint32, disc_convid uint32) {
	l.Warnf("client disconnect tunnelid:%d, convid:%d",tunnelid,disc_convid)
	server,ok := g_server_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect from tunnelid:%d - no such server connection/tunnel",tunnelid)
	}

	//find the kcp connection matching the src address and disconnect/destroy it
	for convid, server_kcp := range server.server_kcps {
		if server_kcp.convid==disc_convid {
			l.Infof("disconnecting kcp convid: %d",server_kcp.convid)
			server_kcp.kcp.Close()
			server_kcp.udp_conn.Close()
			delete(server.server_kcps,convid)
		}
	}	
}

func run_client() {
	g_client_tunnelid=(g_client_tunnelid/10)*10

	l.Errorf("running as client, base tunnel id:%d",g_client_tunnelid)

	
	//create two connections, deprecated, handled by netlink_subscribe_routes
	//client_connect_server(g_client_tunnelid)
	//client_connect_server(g_client_tunnelid)

	//mqtt_check_brokers()
	loopcount := 1

	runthing("ip", "-br", "address")

	go netlink_subscribe_ifaces(g_client_tunnelid)

	// this netlink listener monitors kernel routes, and will connect to the server once a default route is seen,
	// by calling client_connect_server with the details of the route that came up.
	//
	// this is how all connections to a server is established. At startup all existing route information
	// is read by netlink_subscribe_ifaces, so that existing connections are used and initiated.
	go netlink_subscribe_routes(g_client_tunnelid)

	for {
        time.Sleep(10 * time.Millisecond)

        loopcount++

        //debug loop every 2 seconds
        if loopcount%200 == 0 {
            l.Tracef("loop:%d \n", loopcount)
        }
  		//reconnect to mqtt every 5 seconds, if not connected
  		if loopcount%500 == 0 {
			//mqtt_check_brokers()
		}
		
		//send ping to each server kcp every 5 seconds
		if loopcount%500 == 0 {
			client_send_server_pings()
		}

		//show servers every 2 seconds
        if loopcount%200 == 0 {
			l.Warnf("g_server_list: \n%s",printServerList(g_server_list))
			//l.Warnf("g_server_list: \n%+v",g_server_list)
        }

		//recycle every 15 seconds, do occasional stuff
        if loopcount%500 == 0 {
            l.Tracef("loop:%d, housekeeping", loopcount)
            loopcount = 1
			l.Warnf("g_server_list: \n%s",printServerList(g_server_list))
			

			//we poll the routing table every now and again, in case we missed or state messed us arround
			//this allows us to reconnect to the server every so often
			netlink_get_routes(g_client_tunnelid)
        }

	}
}





func run_server() {

	l.Infof("running as server")

	//go netlink_subscribe_ifaces()

	var err error
	
	g_listener, err = createListener()
	l.Infof("listener:%+v",g_listener)
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
	
	

	go server_handle_kcp_listener()
	//go server_listen_tun(iface)


	//mqtt_check_brokers()
	loopcount := 1

	runthing("ip", "-br", "address")

	
	for {
        time.Sleep(10 * time.Millisecond)

        loopcount++

        //debug loop every 2 seconds
        if loopcount%200 == 0 {
            l.Tracef("loop:%d \n", loopcount)
        }
  		//reconnect to mqtt every 5 seconds, if not connected
  		if loopcount%500 == 0 {
			//mqtt_check_brokers()
		}

		//send ping to each client kcp every 5 seconds
		if loopcount%500 == 0 {
			server_send_client_pings()
		}

		//show clients every 2 seconds
        if loopcount%200 == 0 {
			l.Warnf("g_client_list: \n%s",printClientList(g_client_list))
        }

		//recycle every 15 seconds, do occasional stuff
		if loopcount%500 == 0 {
            l.Tracef("loop:%d, housekeeping", loopcount)
			//l.Warnf("g_client_list: %+v",g_client_list)
			l.Warnf("g_client_list: \n%s",printClientList(g_client_list))
            loopcount = 1
        }

	}

}


func create_udp_conn(convid uint32, src string) (*kcp.UDPSession, *net.UDPConn, error) {
	l.Errorf("connecting to %s: convid:%d, src:%s",g_server_addr,convid,src)


	

	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	//block, _ := kcp.NewAESBlockCrypt(key)
	block, _ := kcp.NewNoneBlockCrypt(key)

	//return net.Dial("tcp", g_server_addr)
	//conn,err:=kcp.DialWithOptions(g_server_addr, block, 0, 0)

	//use NewConn3 with convid as agreed per mqtt?
	//https://github.com/xtaci/kcp-go/blob/master/sess.go

	// network type detection
	l_udpaddr, err := net.ResolveUDPAddr("udp", g_server_addr)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}
	
	l_localaddr,err:=net.ResolveUDPAddr("udp",fmt.Sprintf("%s:%d",src,convid))
	if err != nil {
		l.Errorf("unable to bind to local src:%s error:",err)
		return nil,nil, errors.WithStack(err)
	}

	network := "udp4"
	if l_udpaddr.IP.To4() == nil {
		network = "udp4"
	}
	l_localaddr=l_localaddr
	l_localaddr.Port=0 //choose random port

	l.Warnf("connecting with network:%s to:%s",network,l_udpaddr)
	l_conn, err := net.ListenUDP(network, l_localaddr)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}
	l.Errorf("local addr:%s",l_localaddr)

	//create a KCP connection, and specify the UDPConn bound to the src addr
	//It does mean we need to close it ourself later, as kcp won't do that since it doesn't own the UDPConn
	kcp,err:=kcp.NewConn3(convid,l_udpaddr,block,0,0,l_conn)
	return kcp,l_conn,nil
}

func createListener() (*kcp.Listener, error) {
	l.Debugf("createListener")
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	//block, _ := kcp.NewAESBlockCrypt(key)
	block, _ := kcp.NewNoneBlockCrypt(key)
	block=block

	l.Warnf("listening on %s",g_listen_addr)
	//return net.Listen("tcp", g_listen_addr)
	listener,err:=kcp.ListenWithOptions(g_listen_addr, block, 0, 0);
	return listener,err
}

func createTun(ip string,convid uint32) (*water.Interface, error) {
	config := water.Config{
		DeviceType: water.TUN,
	}

	config.Name = fmt.Sprintf("tun%d",convid)

	iface, err := water.New(config)
	if err != nil {
		return nil, err
	}
	l.Infof("Interface Name: %s\n", iface.Name())
	runthing("ip","addr","add",ip+"/30","dev",iface.Name())
	runthing("ip","link","set",iface.Name(),"mtu","1350")
	runthing("ip","link","set",iface.Name(),"up")
	return iface, nil
}


func client_handle_kcp(server *serverType, server_kcp *server_kcp) {
	conn:=server_kcp.kcp
	for {
		fmt.Println("client_handle_kcp")
		message := make([]byte, 1500)
		for {

			kcpstate:=conn.GetState()
			if kcpstate==0xFFFFFFFF {
				//the connection is dead according to kcp
				l.Errorf("kcp convid:%d is dead, closing...",conn.GetConv())
				client_disconnect_session_by_convid(server.base_convid,conn.GetConv())
				return
			}

			
			n, err := conn.Read(message)
			if err != nil {
				l.Error("client_handle_kcp:conn read error:", err)
				client_disconnect_session_by_convid(server.base_convid,conn.GetConv())
				return
			}
			server_kcp.rxcounter++
			if server.iface != nil {
				_, err = server.iface.Write(message[:n])
				if err != nil {
					l.Errorf("client_handle_kcp:tun write err:", err)
				} else {
					l.Tracef("client_handle_kcp:tun write done")
				}
			} else {
				l.Errorf("server.iface is nil")
			}
			if (g_trace) {
				l.Tracef("START - incoming packet (%d bytes) from KCP convid:%d",n,conn.GetConv())
				WritePacket(message[:n])
				l.Tracef("DONE - incoming packet from KCP")
			}
		}

	}
}


func client_ping_server(server *serverType) {
	for convid, server_kcp := range server.server_kcps { // Order not specified 
		l.Tracef("pinging server subconv:%d/%d tun_ip:%s",convid,server_kcp.convid,server.tun_ip)
		pinger, err := probing.NewPinger(server.tun_ip)
		pinger.SetPrivileged(true)
		if err != nil {
			l.Errorf("ping failed:%s",err)
		}
		pinger.Count = 2
		err = pinger.Run() // Blocks until finished.
		if err != nil {
			l.Errorf("ping failed:%s",err)
		}
		stats := pinger.Statistics() 
		l.Tracef("ping stats:%s",stats)
	}
}

func client_send_server_pings() {

	//iterate through the list of clients
	for base_convid, server := range g_server_list { // Order not specified 
		l.Tracef("pinging server:%d",base_convid)
		go client_ping_server(server)

		for convid,server_kcp := range server.server_kcps {
			hello := []byte("\x00\x00")
			if server_kcp.kcp!=nil {
				l.Debugf("sending HELLO to server convid:%d",convid)
				server_kcp.kcp.SetWriteDeadline(time.Now().Add(time.Millisecond*10)) 
				server_kcp.kcp.Write(hello)
			}
		}
	}
}


func client_choose_kcp_conn(server *serverType) (uint32) {
	//server.server_kcps[server.base_convid].kcp
	//basic round robin
	var some_convid uint32=0
	for convid, server_kcp := range server.server_kcps { // Order not specified 
		//fmt.Println(key, value)
		if convid!=server.last_convid && server_kcp.alive {
			server.last_convid=convid
			l.Tracef("chose convid:%d",convid)
			return convid
		}
		some_convid=convid
	}
	//if all else fails, return some conversation id
	l.Tracef("default chose convid:%d",some_convid)
	server.last_convid=some_convid
	return some_convid
}

func client_handle_tun(server *serverType) {
	l.Infof("client_handle_tun:%d",server.base_convid)

	var mypacket packetType	
	mypacket.buff=make([]byte,1500)
	
	if server.iface==nil {
		l.Errorf("cannot handle tun%d, iface not available",server.base_convid)
		return
	}
	//i:=0
	
	for {
		n, err := server.iface.Read(mypacket.buff)
		mypacket.len=n
		if err != nil {
			l.Errorf("tunnel iface read error:", err)
			return
		}

		//l.Tracef("read: %d bytes from %s",n,iface.Name())
		//i++
		
		//l.Tracef("writing to conn1")

		//simplistic round robin
		tryagain:
		chose_convid:=client_choose_kcp_conn(server)
		
		server_kcp,ok:=server.server_kcps[chose_convid] 
		if !ok {
			l.Errorf("server_kcp for chose_convid:%d is not available",chose_convid);
			l.Warnf("g_server_list: \n%s",printServerList(g_server_list))
			continue
		}	
		
		conn:=server_kcp.kcp

		if (conn==nil) {
			l.Errorf("no connection available to send to, waiting a bit")
			time.Sleep(100 * time.Millisecond)
			//TODO: we should probably kick the netlink_subscribe_routes into action again to look for routes
			//or exit entirely and start afresh
			continue
		}

		kcpstate:=conn.GetState()
		if kcpstate==0xFFFFFFFF {
			//the connection is dead according to kcp
			l.Errorf("kcp convid:%d is dead, closing...",conn.GetConv())
			client_disconnect_session_by_convid(server.base_convid,conn.GetConv())

			//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
			goto tryagain;
		}

		//and send the message via the channel to the writer
		//this is potentially  ineficcient, because copying the packetType to channel
		server_kcp.writer_channel <- mypacket

	}
}

func client_kcp_writer(server *serverType, server_kcp *server_kcp, data chan packetType) {

	for pkt := range data {
		//and send it out the selected KCP conn
		server_kcp.kcp.SetWriteDeadline(time.Now().Add(time.Millisecond*10)) 
		_, err := server_kcp.kcp.Write(pkt.buff[:pkt.len])
		if err != nil {
			l.Errorf("conn write error:", err)
			//close the server connection
			client_disconnect_session_by_convid(server.base_convid,server_kcp.convid)

			return //bail out
		}
		server_kcp.txcounter++
	
		if (g_trace) {
			l.Tracef("START - incoming packet (%d bytes) from %s",pkt.len,server.iface.Name())
			WritePacket(pkt.buff[:pkt.len])
			l.Tracef("DONE - incoming packet from %s",server.iface.Name())
		}
	}
}

func setClientConnOptions(conn *kcp.UDPSession) {
	NoDelay, Interval, Resend, NoCongestion := 1, 10, 2, 1 //turbo mode
	//NoDelay, Interval, Resend, NoCongestion := 0, 40, 0, 0 //normal mode
	MTU:=1400
	SndWnd:=256
	RcvWnd:=256
	AckNodelay:=false //this is more speedy
	conn.SetStreamMode(true)
	conn.SetWriteDelay(false)
	conn.SetNoDelay(NoDelay, Interval, Resend, NoCongestion)
	conn.SetMtu(MTU)
	conn.SetWindowSize(SndWnd, RcvWnd)
	conn.SetACKNoDelay(AckNodelay)
}

func setServerConnOptions(conn *kcp.UDPSession) {
	NoDelay, Interval, Resend, NoCongestion := 1, 10, 2, 1 //turbo mode
	//NoDelay, Interval, Resend, NoCongestion := 0, 40, 0, 0 //normal mode
	MTU:=1400
	SndWnd:=256
	RcvWnd:=256
	AckNodelay:=false //this is more speedy
	conn.SetStreamMode(true)
	conn.SetWriteDelay(false)
	conn.SetNoDelay(NoDelay, Interval, Resend, NoCongestion)
	conn.SetMtu(MTU)
	conn.SetWindowSize(SndWnd, RcvWnd)
	conn.SetACKNoDelay(AckNodelay)
}

func server_handle_kcp_listener() {
	
	for {
		conn, err := g_listener.AcceptKCP()
		if err != nil {
			fmt.Println(err)
			return
		}

		

		//TODO: check if the connection was authed via mqtt, and create tun(convid) and assign address
		convid:=conn.GetConv()
		l.Warnf(">>>>>>>>>>>>>>>>>>>>>>>>>>>new connection! convid%d",convid)

		go server_accept_conn(convid,conn)
	}
}

func server_accept_conn(convid uint32, conn *kcp.UDPSession) {

	
	l.Errorf("kcp connection accepted, remote:",conn.RemoteAddr())
	l.Warnf("kcp conv id: %d",convid)

	base_convid:=(convid/10)*10
	l.Warnf("base conv id: %d",base_convid)
	//l.Warnf("g_client_list: %+v",g_client_list)
	l.Warnf("g_client_list: \n%s",printClientList(g_client_list))

	

	//find/add the client in the list
	client,ok:=g_client_list[base_convid]
	if !ok {
		client=new(clientType)
	}
	g_client_list[base_convid]=client

	
	
	//if there's no interface, create the tun for them
	spawn_tun:=false
	if (client.iface == nil) {		
		iface,err:=createTun(g_server_tun_ip,base_convid)
		if err!=nil {
			l.Errorf("error creating tun%d interface :%s",base_convid,err)
		}
		client.iface=iface
		client.tun_ip=g_client_tun_ip
		client.ifname=fmt.Sprintf("tun%d",base_convid)
		client.base_convid=base_convid	

		spawn_tun=true
	}

	
	
	client.last_convid=0 //reset the scheduler

	var kcp_con=new(client_kcp)
	kcp_con.bandwidth=10; kcp_con.rxloss=0; kcp_con.txloss=0; kcp_con.alive=true; //kcp_con.last_hello=0	
	kcp_con.txcounter=0
	kcp_con.priority=0
	
	kcp_con.kcp=conn
	kcp_con.convid=convid
	kcp_con.src_address=fmt.Sprintf("%s",conn.RemoteAddr())


	//create a channel for the kcp channel writer, with a 10 packet buffer
	kcp_con.writer_channel=make(chan packetType,100)
	go server_kcp_writer(client,kcp_con,kcp_con.writer_channel)


	//create the map if required
	if client.client_kcps==nil {
		client.client_kcps=make(map[uint32]*client_kcp)
	}
	client.client_kcps[conn.GetConv()]=kcp_con

	//update the main list
	g_client_list[base_convid]=client
	//l.Warnf("g_client_list: %+v",g_client_list)
	l.Warnf("g_client_list: \n%s",printClientList(g_client_list))

	setServerConnOptions(conn)

	
	//spawn the thread that handles the tunnel
	if spawn_tun {
		go server_handle_tun(client)
	}
	
	//spawn the thread that handles reads on this new kcp conn
	go server_handle_kcp_conn(base_convid,convid,client,kcp_con)
}

func server_handle_kcp_conn(base_convid uint32, convid uint32,client *clientType, kcp_con *client_kcp) {
	conn:=kcp_con.kcp
	//and read forever on the connection
	for {
		
		message := make([]byte, 1500)

		//read forever on the KCP connection, and send it to the tun dev
		for {

			kcpstate:=conn.GetState()
			if kcpstate==0xFFFFFFFF {
				//the connection is dead according to kcp
				l.Errorf("kcp convid:%d is DEAD, closing...",conn.GetConv())
				server_disconnect_session_by_convid(client.base_convid,conn.GetConv())
				return
			}

			n, err := conn.Read(message)
			if err != nil {
				l.Errorf("conn read error:", err)
				//close the client connection
				server_disconnect_session_by_convid(base_convid,convid)
				return
			}

			//check for hello message
			if message[0]==0 && message[1]==0 {
				//this is an initial hello message, don't send it to the tun
				l.Errorf("received HELLO");
				kcp_con.last_hello=time.Now()
				continue;
			}
			//write the packet to the tun
			if client.iface != nil {
				_, err = client.iface.Write(message[:n])
				if err != nil {
					l.Errorf("iface write err:", err)
					//close the client connection
					server_disconnect_session_by_convid(base_convid,convid)
					return
				} else {
					l.Tracef("iface write done")
				}
			}
			if (g_trace) {
				l.Tracef("START - incoming packet (%d bytes) from KCP convid:%d",n,conn.GetConv())
				WritePacket(message[:n])
				l.Tracef("DONE - incoming packet from KCP")
			}
		}

	}
}

func server_disconnect_session_by_convid(tunnelid uint32, disc_convid uint32) {
	l.Warnf("server disconnect tunnelid:%d, convid:%d",tunnelid,disc_convid)
	client,ok := g_client_list[tunnelid]
	if !ok {
		l.Errorf("cannot disconnect tunnelid:%d convid:%d- no such client connection/tunnel",tunnelid,disc_convid)
	}

	//find the kcp connection matching the disc_convid and disconnect/destroy it
	for convid, client_kcp := range client.client_kcps {
		if client_kcp.convid==disc_convid {
			l.Infof("disconnecting tunnelid:%d, kcp convid: %d",tunnelid,client_kcp.convid)
			client.last_convid=0 //restart the round robin
			client_kcp.kcp.Close()
			delete(client.client_kcps,convid)
		}
	}	

	//l.Warnf("AFTER DELETE:g_client_list: %+v",g_client_list)
	l.Warnf("AFTER DELETE: g_client_list: \n%s",printClientList(g_client_list))
}


func server_ping_client(client *clientType) {
	for convid, client_kcp := range client.client_kcps { // Order not specified 
		l.Tracef("pinging client subconv:%d/%d tun_ip:%s",convid,client_kcp.convid,client.tun_ip)
		pinger, err := probing.NewPinger(client.tun_ip)
		pinger.SetPrivileged(true)
		if err != nil {
			l.Errorf("ping failed:%s",err)
		}
		pinger.Count = 2
		err = pinger.Run() // Blocks until finished.
		if err != nil {
			l.Errorf("ping failed:%s",err)
		}
		stats := pinger.Statistics() 
		l.Tracef("ping stats:%s",stats)
	}
}

func server_send_client_pings() {

	//iterate through the list of clients
	for base_convid, client := range g_client_list { // Order not specified 
		l.Tracef("pinging client:%d",base_convid)
		go server_ping_client(client)
	}
}

func server_choose_kcp_conn(client *clientType) (uint32) {
	//l.Warnf("client: %+v",client)
	//basic round robin
	var some_convid uint32=0
	for convid, client_kcp := range client.client_kcps { // Order not specified 
		//fmt.Println(key, value)
		if convid!=client.last_convid && client_kcp.alive {
			client.last_convid=convid
			l.Tracef("chose convid:%d",convid)
			return convid
		}
		some_convid=convid
	}
	//if all else fails, return some or the other convid
	l.Tracef("default chose convid:%d",some_convid)
	client.last_convid=some_convid
	return some_convid
}

func server_handle_tun(client *clientType) {	
	if (client.iface==nil) {
		l.Errorf("cannot handle nonexisting tunnel iface: %d",client.base_convid)
		return
	}

	l.Infof("server_handle_tun:%s",client.iface.Name())
	var mypacket packetType	
	mypacket.buff=make([]byte,1500)
	for {
		
		//find a kcp connection for the client tunnel		
		//cheat, but in future balance accross all conns
		tryagain:
		chose_convid:=server_choose_kcp_conn(client)
		client_kcp,ok:=client.client_kcps[chose_convid] 
		if !ok {
			l.Errorf("client_kcp for chose_convid:%d is not available",chose_convid);
			l.Warnf("g_server_list: \n%s",printServerList(g_server_list))
			time.Sleep(100 * time.Millisecond)
			continue
		}	
		//l.Debugf("chose_convid:%d",chose_convid)
		conn:=client_kcp.kcp
		if (conn==nil) {
			l.Errorf("no connection available to send to, waiting a bit")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		kcpstate:=conn.GetState()
		if kcpstate==0xFFFFFFFF {
			//this connection is dead according to kcp
			l.Errorf("kcp convid:%d is DEAD, closing...",conn.GetConv())
			server_disconnect_session_by_convid(client.base_convid,conn.GetConv())

			//but we carry on handling the tunnel traffic, until something is around . The current packet is getting lost however. //TODO: could try again from top? but that could deadlock
			goto tryagain;
		}

		//read the packet from the wire
		//l.Debugf("read tun")
		n, err := client.iface.Read(mypacket.buff)
		mypacket.len=n
		
		if err != nil {
			l.Panicf("tun iface read error:", err)
			//close the client connection
			//TODO
			return
		}

		//and send the message via the channel to the writer
		//this is potentially  ineficcient, because copying the packetType to channel
		client_kcp.writer_channel <- mypacket

	}
}

func server_kcp_writer(client *clientType, client_kcp *client_kcp, data chan packetType) {
	//l.Debugf("write kcp")
	for pkt := range data {
	
		client_kcp.kcp.SetWriteDeadline(time.Now().Add(time.Millisecond*10))
		_, err := client_kcp.kcp.Write(pkt.buff[:pkt.len])
		if err != nil {
			l.Errorf("kcp conn write error:", err)
			//close the client connection
			server_disconnect_session_by_convid(client.base_convid,client_kcp.kcp.GetConv())
			return
		}
		client_kcp.txcounter++	

		if (g_trace) {
			l.Tracef("START - incoming packet (%d bytes) from %s",pkt.len,client.iface.Name())
			WritePacket(pkt.buff[:pkt.len])
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
	Client struct {
	} `cmd:"" help:"Act as bond client." default:"1"`

	Server struct {
	} `cmd:"" help:"As a bond server."` 
}

func main() {

	
	ctx := kong.Parse(&CLI)
	if (CLI.Debug) {
		g_debug = true
	}
	if (CLI.Trace) {
		g_trace = true
	}

	init_logging()
	l.Infof("main()")

	

	switch ctx.Command() {
		case "client" :	{ 
			run_client()
		}
		case "server" : {
			run_server()
		}
		default: {
			panic(ctx.Command())
		}
	}

}
