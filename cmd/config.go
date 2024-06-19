package main

import (
	"os"

	"github.com/go-yaml/yaml"
	"github.com/pkg/errors"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/totegamma/concurrent/core"
)

type Config struct {
	ApConfig types.ApConfig `yaml:"apConfig"`
	Server   Server         `yaml:"server"`
	NodeInfo types.NodeInfo `yaml:"nodeInfo"`
}

type Server struct {
	Dsn            string `yaml:"dsn"`
	RedisAddr      string `yaml:"redisAddr"`
	RedisDB        int    `yaml:"redisDB"`
	MemcachedAddr  string `yaml:"memcachedAddr"`
	EnableTrace    bool   `yaml:"enableTrace"`
	TraceEndpoint  string `yaml:"traceEndpoint"`
	RepositoryPath string `yaml:"repositoryPath"`
	CaptchaSitekey string `yaml:"captchaSitekey"`
	CaptchaSecret  string `yaml:"captchaSecret"`
}

// Load loads concurrent config from given path
func (c *Config) Load(path string) {
	f, err := os.Open(path)
	if err != nil {
		panic(errors.Wrap(err, "failed to open configuration file"))
	}
	defer f.Close()

	err = yaml.NewDecoder(f).Decode(&c)
	if err != nil {
		panic(errors.Wrap(err, "failed to decode configuration file"))
	}

	c.ApConfig.ProxyCCID, err = core.PrivKeyToAddr(c.ApConfig.ProxyPriv, "con")
	if err != nil {
		panic(errors.Wrap(err, "failed to generate proxy CCID"))
	}
}
