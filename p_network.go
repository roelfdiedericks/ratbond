package main

import (
	"net"	
	"strings"
	"golang.org/x/net/icmp"
    "golang.org/x/net/ipv4"
	"time"
	"os"
)

var g_mac string = ""
var g_lan_ip string = ""
var g_lan_broadcast string = ""
var g_broadcast_setup bool = false


func network_getMacAddr(name string) ([]string, error) {
	ifas, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var as []string
	for _, ifa := range ifas {
		a := ifa.HardwareAddr.String()
		n := ifa.Name
		if a != "" {
			if n == name {
				as = append(as, a)
			}
		}
	}
	return as, nil
}

func network_interfaceExists(name string) bool {
	ifas, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifa := range ifas {

		n := ifa.Name
		if n != "" {
			if n == name {
				return true
			}
		}
	}
	return false
}
func network_getIpAddr(name string) ([]string, error) {
	ifas, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var as []string
	for _, ifa := range ifas {

		n := ifa.Name
		if n != "" {
			if n == name {
				ifaddr, err := ifa.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range ifaddr {
					var ip net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					default:
						continue
					}
					ipv4 := ip.To4()
					if ipv4 != nil {
						as = append(as, ipv4.String())
					}
					return as, nil
				}
			}
		}
	}
	return as, nil
}

func network_ifAddrExists(name string, find_addr string) bool {
	ifas, err := net.Interfaces()
	if err != nil {
		return false
	}

	for _, ifa := range ifas {

		n := ifa.Name
		if n != "" {
			if n == name {
				ifaddr, err := ifa.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range ifaddr {
					var ip net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					default:
						continue
					}
					ipv4 := ip.To4()
					if ipv4.String() == find_addr {
						return true
					}
				}
			}
		}
	}
	return false
}

func network_get_lan_ip() {
	g_lan_ip = ""
	//try br1, then br-lan
	ips, err := network_getIpAddr("br1")
	if err != nil {
		l.Fatal(err)
	}
	for _, ip := range ips {
		g_lan_ip = ip
	}
	if g_lan_ip != "" {
		l.Infof("My br1 lan_ip address:%s", g_lan_ip)
	} else {
		ips, err := network_getIpAddr("br-lan")
		if err != nil {
			l.Fatal(err)
		}
		for _, ip := range ips {
			g_lan_ip = ip
		}
		if g_lan_ip != "" {
			l.Infof("My br-lan lan_ip address:%s", g_lan_ip)
		} else {
			l.Errorf("unable to find br-lan or br1 ip addresses, will try later")
		}
	}
}
func network_get_mac() string {
	//get eth0 mac addy
	as, err := network_getMacAddr("eth0")
	if err != nil {
		l.Fatal(err)
	}
	for _, a := range as {
		g_mac = a
	}
	l.Debugf("My mac address:%s", g_mac)
	return strings.ToUpper(g_mac)
}


func network_sysctls () {
	//set a few sysctls to increase send/receive buffer sizes
	runthing("sysctl","net.core.rmem_max=26214400")
	runthing("sysctl","net.core.rmem_default=26214400")
	runthing("sysctl","net.core.wmem_max=26214400")
	runthing("sysctl","net.core.wmem_default=26214400")
	runthing("sysctl","net.core.netdev_max_backlog=2048")

	runthing("sysctl","net.ipv4.ping_group_range=0 2147483647") //allow us to do unprivileged pings
}


const (
    proto_id_ICMPv4 = 1
	proto_icmp_header_size=8
	proto_ipv4_header_size=20
	proto_udpv4_header_size=8
)

func ping_probe(host string,src_address string,len int,seq int) {

	l.Infof("sendping ping len:%d to %s, src:%s",len,host,src_address)
    // Use icmp to get a *packetconn, note that we set `udp4` for network here
    c, err := icmp.ListenPacket("udp4", src_address)
    if err != nil {
        l.Errorf("listenpacket error:%s",err)
		return
    }
    defer c.Close()
    // Generate an Echo message
	packet := make([]byte, len)
    msg := &icmp.Message{
        Type: ipv4.ICMPTypeEcho,
        Code: 0,
        Body: &icmp.Echo{
            ID:   os.Getpid() & 0xffff,
            Seq:  seq,
            Data: packet,
        },
    }
	
    wb, err := msg.Marshal(nil)
    if err != nil {
		l.Errorf("marshall error:%s",err)
		return
    }

    // Send, note that here it must be a UDP address
    start := time.Now()
    if _, err := c.WriteTo(wb, &net.UDPAddr{IP: net.ParseIP(host)}); err != nil {
        l.Errorf("writeto error:%s",err)
    }
    // Read the reply package
    reply := make([]byte, 1500)
    err = c.SetReadDeadline(time.Now().Add(5 * time.Second))
    if err != nil {
        l.Errorf("setreaddeadline error:%s",err)
		return
    }
    n, peer, err := c.ReadFrom(reply)
    if err != nil {
        l.Errorf("read error:%s",err)
		return
    }
    duration := time.Since(start)

    // The reply packet is an ICMP message, parsed first
    msg, err = icmp.ParseMessage(proto_id_ICMPv4, reply[:n])
    if err != nil {
        l.Errorf("parsemessage error:%s, len:%d",err, n)
		return
    }
    // Print Results
    switch msg.Type {
    case ipv4.ICMPTypeEchoReply: // If it is an Echo Reply message
        echoReply, ok := msg.Body.(*icmp.Echo) // The message body is of type Echo
        if !ok {
            l.Errorf("invalid ICMP Echo Reply message")
            return
        }
        // Here you can judge by ID, Seq, remote address, the following one only uses two judgment conditions, it is risky
        // If another program also sends ICMP Echo with the same serial number, then it may be a reply packet from another program, but the chance of this is relatively small
        // If you add the ID judgment, it is accurate
        if peer.(*net.UDPAddr).IP.String() == host && echoReply.Seq == seq {
            l.Infof("Reply from %s (src:%s): seq=%d time=%v, len=%d\n", host,src_address, msg.Body.(*icmp.Echo).Seq, duration,n)
            return
        } else {
			l.Warnf("Unexpected Reply from %s (src:%s): seq=%d time=%v, len=%d\n", host,src_address, msg.Body.(*icmp.Echo).Seq, duration,n)
		}
    default:
        l.Errorf("Unexpected ICMP message type: %v\n", msg.Type)
    }
}