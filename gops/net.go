package gops

import "github.com/duxlabs-dev/dux-gop/models"

func (self *GopsUtil) GetNetworkInfo() ([]*models.NetworkInfo, error) {
	netIO, err := self.netProvider.IOCounters(true)
	res := make([]*models.NetworkInfo, 0)
	if err == nil {
		for _, n := range netIO {
			// Filter to match bash script (wlan, wlo, wlp, eth, eno, enp, ens, lxc, wld)
			if matchesNetworkInterface(n.Name) {
				res = append(res, &models.NetworkInfo{
					Name: n.Name,
					Rx:   n.BytesRecv,
					Tx:   n.BytesSent,
				})
			}
		}
	}
	return res, nil
}
