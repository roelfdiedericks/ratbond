package main

import (
	"fmt"
	"net"
	"time"
	"sort"
	"io"
	"net/http"
	"context"
	"github.com/roelfdiedericks/kcp-go"
	
)

func ByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func ByteCountBinary(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}


func reverse(less func(i, j int) bool) func(i, j int) bool {
	return func(i, j int) bool {
		return !less(i, j)
	}
}


func printServerList(list map[uint32]*serverType)  (string) {
	s:=""
	for serverid, server := range list {
		s+=printServer(server,serverid)
	}
	return s
}



func printServerLoads(list map[uint32]*serverType)  (string) {
	s:=""
	for serverid, server := range list {
		s+=fmt.Sprintf("members:%d { %+v }",serverid,server.consistent.Members())
	}
	return s
}

func printClientLoads(list map[uint32]*clientType)  (string) {
	s:=""
	for clientid, client := range list {
		s+=fmt.Sprintf("members:%d { %+v }",clientid,client.consistent.Members())
	}
	return s
}

/*
type server struct {
	server_kcps map[uint32] server_kcp
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	tun_ip string

	reorder_buffer []packet
}
*/
func printServer(srv *serverType,serverid uint32 ) (string) {
	s:=fmt.Sprintf("server:%d \n{ \nbase_convid=%d, \n",serverid,srv.base_convid)
	s+=fmt.Sprintf("last_convid=%d, \n",srv.last_convid)
	s+=fmt.Sprintf("ifname=%s, \n",srv.ifname)
	s+=fmt.Sprintf("iface=%p, \n",srv.iface)
	s+=fmt.Sprintf("my_tun_ip=%s, \n",srv.my_tun_ip)
	s+=fmt.Sprintf("remote_tun_ip=%s, \n",srv.remote_tun_ip)
	s+="\nconnections={ \n\n"

	keys := make([]int, 0)
	for k, _ := range srv.connections {
    	keys = append(keys, int(k))
	}
	sort.Ints(keys)
	for _, convid := range keys {
		s+=fmt.Sprintf("  convid=%d ",convid)
		s+=printServerConnection(srv.connections[uint32(convid)])
		s+="  \n"
	}	
	s+="}\n"
	return s
}

/*
type server_kcp struct {
	convid uint32
	kcp *kcp.UDPSession
	udp_conn *net.UDPConn
	rxloss uint32
	txloss uint32
	txcounter uint64
	priority uint32
	bandwidth uint32

	last_hello time.Time
	alive bool
	src_address string
}
*/

func printSnmp(snmp *kcp.Snmp) string {
	names:=snmp.Header()
	values:=snmp.ToSlice()
	s:="\t\tsnmp: { \n"
	for n, name := range names {
		s+=fmt.Sprintf("\t\t\t%s=%s\n",name,values[n])
	}

	s+="\t\t}\n"
	return s
}

func printServerConnection(k *serverConnection) (string) {
	s:=fmt.Sprintf("\n\t{\n\tconvid=%d, \n",k.convid)
	if k.session==nil {
		return fmt.Sprintf("\tsession=nil\n")
	}
	if k.session!=nil {
		s+=fmt.Sprintf("\tkcp=%p, \n",k.session.kcp)
	} else {
		s+=fmt.Sprintf("\tkcp=nil, \n")
	}
	s+=fmt.Sprintf("\tkcpstate=%d, \n",k.session.kcp.GetState())
	s+=fmt.Sprintf("\tkcp_rto=%d, \n",k.session.kcp.GetRTO())
	s+=fmt.Sprintf("\tudp_conn=%p, \n",k.session.udp_conn)
	s+=fmt.Sprintf("\tifname=%s, \n",k.ifname)
	s+=fmt.Sprintf("\ttxcounter=%d, \n",k.txcounter)
	s+=fmt.Sprintf("\trxcounter=%d, \n",k.rxcounter)
	s+=fmt.Sprintf("\trxtimeouts=%d, \n",k.rxtimeouts)
	s+=fmt.Sprintf("\ttxtimeouts=%d, \n",k.txtimeouts)
	s+=fmt.Sprintf("\tpriority=%d, \n",k.priority)
	s+=fmt.Sprintf("\ttxbandwidth=%.2f, \n",k.txbandwidth)
	s+=fmt.Sprintf("\trxbandwidth=%.2f, \n",k.rxbandwidth)
	
	ut := time.Now()
	uptime := ut.Sub(k.up_since).Seconds()
	s+=fmt.Sprintf("\tuptime=%.2f, \n",uptime)

	
	t1 := time.Now()
	diff := t1.Sub(k.last_hello).Seconds()
	s+=fmt.Sprintf("\thello_age=%.2f, \n",diff)

	s+=fmt.Sprintf("\talive=%t, \n",k.alive)
	s+=fmt.Sprintf("\tkcp_mtu=%d, \n",k.kcp_mtu)
	s+=fmt.Sprintf("\tnext_mtu=%d, \n",k.next_mtu)
	s+=fmt.Sprintf("\tsrc_address=%s, \n",k.src_address)
	s+=fmt.Sprintf("\twan_ip=%s, \n",k.wan_ip)

	s+=fmt.Sprintf("\tup_since=%s, \n",k.up_since.Format("20060102-15:04:05.000"))
	s+=fmt.Sprintf("\tlast_hello=%s \n",k.last_hello.Format("20060102-15:04:05.000"))

	s+=printSnmp(k.session.kcp.GetSnmp())

	s+="\t}\n"

	
	return s
}



// --------------------------------------------
func printClientList(list map[uint32]*clientType)  (string) {
	s:=""
	for clientid, client := range list {
		s+=printClient(client,clientid)
	}
	return s
}

/*
type clientType struct {
	client_kcps map[uint32] *client_kcp
	iface *water.Interface
	base_convid uint32
	last_convid uint32
	ifname string
	tun_ip string
	reorder_buffer []packet
}

*/
func printClient(cl *clientType,clientid uint32 ) (string) {
	s:=fmt.Sprintf("client:%d { base_convid=%d, ",clientid,cl.base_convid)
	s+=fmt.Sprintf("last_convid=%d, ",cl.last_convid)
	s+=fmt.Sprintf("ifname=%s, ",cl.ifname)
	s+=fmt.Sprintf("iface=%p, ",cl.iface)
	s+=fmt.Sprintf("my_tun_ip=%s, ",cl.my_tun_ip)
	s+=fmt.Sprintf("remote_tun_ip=%s, ",cl.remote_tun_ip)
	s+="\n connections={ \n"
	keys := make([]int, 0)
	for k, _ := range cl.connections {
    	keys = append(keys, int(k))
	}
	sort.Ints(keys)
	for _, convid := range keys {
		s+=fmt.Sprintf("\tconvid=%d ",convid)
		s+=printClientConnection(cl.connections[uint32(convid)])
		s+="  \n"
	}
	s+="}\n"
	
	return s
}

/*
type client_kcp struct {
	convid uint32
	kcp *kcp.UDPSession
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

*/
func printClientConnection(k *clientConnection) (string) {
	s:=fmt.Sprintf("{ convid=%d, ",k.convid)
	s+=fmt.Sprintf("kcp=%p, ",k.session.kcp)
	s+=fmt.Sprintf("kcpstate=%d, ",k.session.kcp.GetState())
	s+=fmt.Sprintf("kcp_rto=%d, ",k.session.kcp.GetRTO())
	//s+=fmt.Sprintf("udp_conn=%p, ",k.udp_conn)
	s+=fmt.Sprintf("txcounter=%d, ",k.txcounter)
	s+=fmt.Sprintf("rxcounter=%d, ",k.rxcounter)
	s+=fmt.Sprintf("rxtimeouts=%d, ",k.rxtimeouts)
	s+=fmt.Sprintf("txtimeouts=%d, ",k.txtimeouts)
	s+=fmt.Sprintf("priority=%d, ",k.priority)
	s+=fmt.Sprintf("txbandwidth=%.2f, ",k.txbandwidth)
	s+=fmt.Sprintf("rxbandwidth=%.2f, ",k.rxbandwidth)
	s+=fmt.Sprintf("alive=%t, ",k.alive)
	s+=fmt.Sprintf("kcp_mtu=%d, ",k.kcp_mtu)
	
	ut := time.Now()
	uptime := ut.Sub(k.up_since).Seconds()
	s+=fmt.Sprintf("uptime=%.2f, ",uptime)

	
	t1 := time.Now()
	diff := t1.Sub(k.last_hello).Seconds()
	s+=fmt.Sprintf("hello_age=%.2f, ",diff)
	s+=fmt.Sprintf("alive=%t, ",k.alive)
	s+=fmt.Sprintf("src_address=%s, ",k.src_address)
	

	s+=fmt.Sprintf("up_since=%s, ",k.up_since.Format("20060102-15:04:05.000"))
	s+=fmt.Sprintf("last_hello=%s ",k.last_hello.Format("20060102-15:04:05.000"))

	
	s+=" }"
	s+=printSnmp(k.session.kcp.GetSnmp())
	return s
}


func IpHosts(cidr string) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	var ips []string
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); ipinc(ip) {
		ips = append(ips, ip.String())
	}
	
	// remove network address and broadcast address
	lenIPs := len(ips)
	switch {
	case lenIPs < 2:
		return ips, nil
	
	default:
	return ips[1 : len(ips)-1], nil
	}
}

func ipinc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}


func get_wan_ip(iface string) (string, error) {
	ip,err:=network_getIpAddr(iface)
	if err!=nil {
		l.Errorf("unable to get ip for iface:%s error:%s",iface,err)
		return "",err
	}
	if ip[0]=="" {
		l.Errorf("unable to get ip for iface:%s got:%s",iface,ip[0])
	}
	bind_ip:=ip[0]+":0"
	addr, err := net.ResolveTCPAddr("tcp", bind_ip)
	if err!=nil {
		l.Errorf("unable to ind to ip:%s for iface:%s error:%s",ip,iface)
		return "",err
	}
	dialer := &net.Dialer{LocalAddr: addr}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialer.Dial(network, addr)
		return conn, err
	}
	transport := &http.Transport{DialContext: dialContext}
	if transport==nil {
		l.Errorf("transport was nil!")
		return "",err
	}
	client := &http.Client{
		Transport: transport,
	}
	if client==nil {
		l.Errorf("client was nil!")
		return "",err
	}
	
	// http request
	response, err := client.Get("https://api.ipify.org/") // get my IP address
	if err!=nil {
		l.Errorf("http request to api.ipfiy.org failed: error:%s",err)
		return "",err
	}
	//TODO: should probably check if the data is somewhat valid, and to detect mobile captive portal type stuff like Vodacom sending a 302
	data, err := io.ReadAll(response.Body);
	wan_ip:=fmt.Sprintf("%s",data)
	l.Infof("wan ip: %s for iface:%s from api.ipify.org",wan_ip,iface)
	return wan_ip,nil
}