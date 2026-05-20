package antiban

import "golang.org/x/net/proxy"

// ProxyDialer returns a default dialer.
func ProxyDialer() proxy.Dialer { return proxy.Direct }
