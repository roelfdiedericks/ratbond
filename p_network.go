package main

import (
	"net"	
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
func network_get_mac() {
	//get eth0 mac addy
	as, err := network_getMacAddr("eth0")
	if err != nil {
		l.Fatal(err)
	}
	for _, a := range as {
		g_mac = a
	}
	l.Debugf("My mac address:%s", g_mac)
}
