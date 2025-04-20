package main

import (
	"github.com/concrnt/ccworld-ap-bridge/types"
)

type Config struct {
	ApConfig types.ApConfig `yaml:"apConfig"`
	Server   Server         `yaml:"server"`
	NodeInfo types.NodeInfo `yaml:"nodeInfo"`
}

type Server struct {
	Dsn           string `yaml:"dsn"`
	GatewayAddr   string `yaml:"gatewayAddr"`
	RedisAddr     string `yaml:"redisAddr"`
	RedisDB       int    `yaml:"redisDB"`
	MemcachedAddr string `yaml:"memcachedAddr"`
	EnableTrace   bool   `yaml:"enableTrace"`
	TraceEndpoint string `yaml:"traceEndpoint"`
	ApiAddr       string `yaml:"apiAddr"`
}
