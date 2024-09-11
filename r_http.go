package main
import (
	"fmt"
	"net/http"	
	
)


func http_status(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type","text/html")
	if g_run_client {
		fmt.Fprintf(w, "<pre>g_server_list: \n%s",printServerList(g_server_list))
	}
	if g_run_server {
		fmt.Fprintf(w, "<pre>client_list: \n%s",printClientList(g_client_list))
	}
}

func http_serve() {
	if g_http_listen_addr=="" {
		l.Infof("http listening disabled")
		return
	}
	http.HandleFunc("/status", http_status)
	
	l.Infof("listening for http requests on %s",g_http_listen_addr)
	http.ListenAndServe(g_http_listen_addr, nil)
}

