package main
import (
	"fmt"
	"time"
	"net"
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
		s+=fmt.Sprintf("loads:%d { %+v }",serverid,server.consistent.GetLoads())
	}
	return s
}

func printClientLoads(list map[uint32]*clientType)  (string) {
	s:=""
	for clientid, client := range list {
		s+=fmt.Sprintf("loads:%d { %+v }",clientid,client.consistent.GetLoads())
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
	s:=fmt.Sprintf("server:%d { base_convid=%d, ",serverid,srv.base_convid)
	s+=fmt.Sprintf("last_convid=%d, ",srv.last_convid)
	s+=fmt.Sprintf("ifname=%s, ",srv.ifname)
	s+=fmt.Sprintf("iface=%p, ",srv.iface)
	s+=fmt.Sprintf("my_tun_ip=%s, ",srv.my_tun_ip)
	s+=fmt.Sprintf("remote_tun_ip=%s, ",srv.remote_tun_ip)
	s+="\n connections={ \n"
	for convid, connection := range srv.connections {
		s+=fmt.Sprintf("\tconvid=%d ",convid)
		s+=printServerConnection(connection)
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
func printServerConnection(k *serverConnection) (string) {
	s:=fmt.Sprintf("{ convid=%d, ",k.convid)
	if k.session==nil {
		return fmt.Sprintf("session=NIL!!!")
	}
	if k.session!=nil {
		s+=fmt.Sprintf("kcp=%p, ",k.session.kcp)
	} else {
		s+=fmt.Sprintf("kcp=NIL!!!")
	}
	s+=fmt.Sprintf("kcpstate=%d, ",k.session.kcp.GetState())
	s+=fmt.Sprintf("udp_conn=%p, ",k.session.udp_conn)
	s+=fmt.Sprintf("ifname=%s, ",k.ifname)
	s+=fmt.Sprintf("txcounter=%d, ",k.txcounter)
	s+=fmt.Sprintf("rxcounter=%d, ",k.rxcounter)
	s+=fmt.Sprintf("rxtimeouts=%d, ",k.rxtimeouts)
	s+=fmt.Sprintf("txtimeouts=%d, ",k.txtimeouts)
	s+=fmt.Sprintf("priority=%d, ",k.priority)
	s+=fmt.Sprintf("txbandwidth=%.2f, ",k.txbandwidth)
	s+=fmt.Sprintf("rxbandwidth=%.2f, ",k.rxbandwidth)
	
	ut := time.Now()
	uptime := ut.Sub(k.up_since).Seconds()
	s+=fmt.Sprintf("uptime=%.2f, ",uptime)

	
	t1 := time.Now()
	diff := t1.Sub(k.last_hello).Seconds()
	s+=fmt.Sprintf("hello_age=%.2f, ",diff)

	s+=fmt.Sprintf("alive=%t, ",k.alive)
	s+=fmt.Sprintf("src_address=%s, ",k.src_address)

	s+=fmt.Sprintf("up_since=%s, ",k.up_since.Format("20060102-15:04:05.000"))
	s+=fmt.Sprintf("last_hello=%s",k.last_hello.Format("20060102-15:04:05.000"))

	s+=" }"
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
	for convid, connection := range cl.connections {
		s+=fmt.Sprintf("\tconvid=%d ",convid)
		s+=printClientConnection(connection)
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
	//s+=fmt.Sprintf("udp_conn=%p, ",k.udp_conn)
	s+=fmt.Sprintf("txcounter=%d, ",k.txcounter)
	s+=fmt.Sprintf("rxcounter=%d, ",k.rxcounter)
	s+=fmt.Sprintf("rxtimeouts=%d, ",k.rxtimeouts)
	s+=fmt.Sprintf("txtimeouts=%d, ",k.txtimeouts)
	s+=fmt.Sprintf("priority=%d, ",k.priority)
	s+=fmt.Sprintf("txbandwidth=%.2f, ",k.txbandwidth)
	s+=fmt.Sprintf("rxbandwidth=%.2f, ",k.rxbandwidth)
	s+=fmt.Sprintf("alive=%t, ",k.alive)
	
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
