package models

type NetworkInfo struct {
	Name string `json:"name"`
	Rx   uint64 `json:"rx"`
	Tx   uint64 `json:"tx"`
}

type NetworkRateInfo struct {
	Interface string  `json:"interface"`
	RxRate    float64 `json:"rxrate"`
	TxRate    float64 `json:"txrate"`
	RxTotal   uint64  `json:"rxtotal"`
	TxTotal   uint64  `json:"txtotal"`

	MTU      int      `json:"mtu,omitempty"`
	Speed    int      `json:"speed,omitempty" doc:"Link speed in Mbit/s; absent when unknown"`
	Duplex   string   `json:"duplex,omitempty"`
	LinkType string   `json:"linkType,omitempty" doc:"ethernet, wireless, tunnel, loopback or other"`
	Driver   string   `json:"driver,omitempty"`
	IPv4     []string `json:"ipv4,omitempty" doc:"CIDR"`
	IPv6     []string `json:"ipv6,omitempty" doc:"CIDR"`
}

type NetworkRateResponse struct {
	Interfaces []*NetworkRateInfo `json:"interfaces"`
	Cursor     string             `json:"cursor"`
}
