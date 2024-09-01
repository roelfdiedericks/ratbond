package main
import (
	"github.com/vishvananda/netlink"
	"fmt"
	"net"
	"strings"
	"golang.org/x/sys/unix"
)

var g_netlink_iface_updates chan netlink.LinkUpdate
var g_netlink_route_updates chan netlink.RouteUpdate

func netlink_subscribe_ifaces(tunnelid uint32) {
	l.Warnf("monitoring interfaces")
	g_netlink_iface_updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)
	if err := netlink.LinkSubscribe(g_netlink_iface_updates, done); err != nil {
		l.Errorf("subscribe_netlink error:%s", err)

	}
	for update := range g_netlink_iface_updates {
		ifname:=fmt.Sprintf("%s",update.Link.Attrs().Name)
		if strings.Contains(ifname,"tun") {
			l.Debugf("ignoring tun interface:%s",ifname)
			continue
		}

		l.Errorf("Interface iface:%s, state:%s", update.Link.Attrs().Name, update.Link.Attrs().OperState.String())
		if update.Link.Attrs().OperState.String() == "up" {
		}

		if update.Link.Attrs().OperState.String() == "down" {	
			client_disconnect_session_by_ifname(tunnelid,ifname)
		}
	}
}


func handle_route_update(update netlink.RouteUpdate, tunnelid uint32) {
	l.Debugf("netlink ->>>>>>>>>>>>> type: %d linkindex:%d dst:%s, src:%s, gw:%s %s", update.Type, update.Route.LinkIndex, update.Route.Dst,update.Route.Src,update.Route.Gw,update.Route)

	//we're only interested in default routes
	dst:=fmt.Sprintf("%s",update.Route.Dst)
	if (update.Type==unix.RTM_NEWROUTE && dst=="0.0.0.0/0") {			
		
		l.Debugf("NEW DEFAULT ROUTE linkindex:%d dst:%s, src:%s, gw:%s dump:%s", update.Route.LinkIndex, update.Route.Dst,update.Route.Src,update.Route.Gw,update.Route)
		if (update.Route.Table==254) {
			l.Debugf("RTM_NEWROUTE:Ignoring default route in main table")	
			return
		}
		if (update.Route.Src==nil) {
			l.Errorf("Unable to use default route, src address is empty!: linkindex:%d dst:%s, src:%s, gw:%s", update.Route.LinkIndex, update.Route.Dst,update.Route.Src,update.Route.Gw)
		} else {
			rif,err:=net.InterfaceByIndex(update.Route.LinkIndex)
			if (err!=nil) {
				l.Errorf("Unable to use default route, InterfaceByIndex failed:%s",err)	
			}
			client_connect_server(tunnelid,fmt.Sprintf("%s",update.Route.Src),rif.Name,fmt.Sprintf("%s",update.Route.Gw),"NEW DEFAULT ROUTE DETECTED")
			
		}
	}
	if (update.Type==unix.RTM_DELROUTE && dst=="0.0.0.0/0") {
		l.Debugf("REMOVED DEFAULT ROUTE linkindex:%d dst:%s, src:%s, gw:%s dump:%s", update.Route.LinkIndex, update.Route.Dst,update.Route.Src,update.Route.Gw,update.Route)
		if (update.Route.Table==254) {
			l.Debugf("RTM_DELROUTE:Ignoring default route in main table:%s")	
			return
		}
		if (update.Route.Src==nil) {
			l.Errorf("Unable to handle remove default route, src address is empty!: linkindex:%d dst:%s, src:%s, gw:%s", update.Route.LinkIndex, update.Route.Dst,update.Route.Src,update.Route.Gw)
		} else {
			rif,err:=net.InterfaceByIndex(update.Route.LinkIndex)
			if (err!=nil) {
				l.Errorf("Unable to handle remove default route, InterfaceByIndex failed:%s",err)	
			}
			client_disconnect_session_by_src(tunnelid,fmt.Sprintf("%s",update.Route.Src),rif.Name,fmt.Sprintf("%s",update.Route.Gw))
		}
	}
}

func netlink_subscribe_routes(tunnelid uint32) {
	l.Infof("monitoring routes tunnelid:%d",tunnelid)
	g_netlink_route_updates := make(chan netlink.RouteUpdate)
	done := make(chan struct{})
	defer close(done)
	if err := netlink.RouteSubscribeWithOptions(g_netlink_route_updates, done, netlink.RouteSubscribeOptions{ListExisting:true}); err != nil {
		l.Errorf("subscribe_routes error: ",err)
	}
	for update := range g_netlink_route_updates {	
		handle_route_update(update,tunnelid)
	}
}


func netlink_get_routes(tunnelid uint32) {
	l.Debugf("getting routes, once-off tunnelid:%d",tunnelid)
	route_updates := make(chan netlink.RouteUpdate)
	done := make(chan struct{})
	var timeout unix.Timeval
	timeout.Sec=1
	timeout.Usec=100
	defer close(done)
	if err := netlink.RouteSubscribeWithOptions(route_updates, done, netlink.RouteSubscribeOptions{ListExisting:true,ReceiveTimeout: &timeout}); err != nil {
		l.Errorf("subscribe_routes error: ",err)
	}
	for update := range route_updates {	
		handle_route_update(update,tunnelid)
	}
	l.Tracef("received all updates....")
}

