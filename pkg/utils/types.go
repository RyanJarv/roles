package utils

type PolicyDocument struct {
	Version   string            `json:"Version"`
	Statement []PolicyStatement `json:"Statement"`
}

type PolicyStatement struct {
	Sid       string          `json:"Sid"`
	Effect    string          `json:"Effect"`
	Principal PolicyPrincipal `json:"Principal"`
	Action    string          `json:"Action"`
	Resource  string          `json:"Resource,omitempty"`
}

type PolicyPrincipal struct {
	AWS string `json:"AWS"`
}

type Info struct {
	Comment string
	Exists  bool
}
