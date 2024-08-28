# ratbond - wan bonding solution written in go 

ratbond is a WAN bonding solution that can aggregate multiple WAN links into a single interface to achieve
combined higher throughput, and link redundancy. 


## Concept 
ratbond connects Linux TUN interfaces to each other using the KCP protocol over UDP to manage connections and packet retransmission/ordering. Multiple WAN interfaces can be used  to create a bonded link.

ratbond operates in either server mode (typically hosted on a VPS), or client mode (typically run on an embedded router or other device with WAN links).

A configurable point-to-point, e.g. 10.10.10.0/30 IP address is used to define the IP address for the tun interface on client and server. ratbond will automatically assign an IP to server and one to the client.

ratbond binds the KCP connection of each WAN link on the client to the IP address of the WAN link so that the source of the KCP packets is each individual link. 
This does require configuring some [policy routing](#policyrouting) on the client to ensure that packets from the WAN ip is only sent via the WAN links' default gateway

KCP connections can optionally be encrypted using the AES cipher, and a pre-shared key.

The overheads of a ratbond tunnel is around 15% of the link capacity.

ratbond uses netlink [linkstate](#linkstate) monitoring to dynamically add or remove WAN members from the bond.

## Building
- golang version 1.23.0 or above is recommended.
- ratbond uses golang vendoring (for the simple reason to avoid conflicts with other go programs)
- ratbond uses a forked version of kcp-go https://github.com/roelfdiedericks/kcp-go with some added functionality
```
    go get        #get all dependencies
    ./buildit.sh  #will produce binaries
```
- binaries are produced for x86, arm64, mipsle, and mipsbe 

## <a name="policyrouting"></a> Policy Routing is REQUIRED
Policy routing on each WAN interface is required in order to ensure that ratbond packets sent out a specific WAN interface only uses the WAN interface's default route.

This is called "source-based policy routing".

Source-based policy routing is a requirement for ratbond to function properly, and cannot be avoided. In future, ratbond might create the policy routing rules itself, but for now it's out of scope.

## Configuring Policy Routing

### OpenWRT
On OpenWRT, this can be easily achieved by setting a unique "Override IPv4 routing table" value on the Interfaces->wan1->Edit->Advanced Settings TAB in the OpenWRT GUI
You have to set a unique "route table" numerical value on each WAN interface. Using OpenWRT with route tables, is the easiest way to get started with ratbond.

### Regular Linux
On regular linux, you need to manipulate the routing rules to achieve something similar to the following (example of two WAN links, eth1=6 and eth2=7).
Dynamically achieving this when WAN links go up/down is outside the scope of this documentation, as it becomes a bit more complicated.

```
ip rule ls

0:      from all lookup local
10000:  from 192.168.0.193 lookup 6
10000:  from 192.168.88.251 lookup 7

ip route show table 6
default via 192.168.0.1 dev eth1 proto static src 192.168.0.193 metric 6
192.168.0.0/24 dev eth1 proto static scope link metric 6

ip route show table 7
default via 192.168.88.1 dev eth2 proto static src 192.168.88.251 metric 5
192.168.88.0/24 dev eth2 proto static scope link metric 5

```

Your routing table should then only show your directly attached LAN routes, and no default gateway at all
```
ip route
192.168.99.0/24 dev eth0 proto kernel scope link src 192.168.99.14
```
## <a name="linkstate"></a>WAN Links
Any interface with a default route, that is not in the "main" linux routing table (table 254) is considered a "WAN" link. In future some filtering/specific definitions of WAN links will be implemented, but for now any link with default route in it's own unique routing table is considered "WAN".

When ratbond starts up, it scans the routing table ands adds detected "WAN" links to the bond, and then continues monitoring routes/interfaces. The routing table is also scanned every 15 seconds, in case an event was missed.

## <a name="linkstate"></a>Link state detection
ratbond uses netlink routing table and interface table monitoring to detect when links are up or down. When a link goes down (interface down event), or a default route on a WAN interface disappears (route down event), ratbond removes the interface as a bonding member.

When the link is restored, and a default route becomes available again the link is added back again into the bond