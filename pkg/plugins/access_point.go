package plugins

func NewAccessPoint() *AccessPoint {
	return &AccessPoint{}
}

type AccessPoint struct {
	Arn string
}

func (a *AccessPoint) Concurrency() int { return 5 }
