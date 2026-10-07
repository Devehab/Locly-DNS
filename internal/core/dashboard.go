package core

// DashboardHostname is the name LocalDNS gives its own web interface, so
// people open http://localdns.local (through the router) instead of an IP.
// `localdns router enable` maps it to 127.0.0.1:DashboardPort.
const DashboardHostname = "localdns.local"

// DashboardPort is the web interface's default port.
const DashboardPort = 7357

// DashboardAddress is the address DashboardHostname points to.
const DashboardAddress = "127.0.0.1:7357"
