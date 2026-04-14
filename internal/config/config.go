// Package config parses matrix.config.yaml and resolves per-service environment lists.
// Merge precedence (lowest → highest): global < environment < service.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MatrixConfig is the top-level structure of matrix.config.yaml.
type MatrixConfig struct {
	Global      GlobalConfig                 `yaml:"global"`
	Environment map[string]EnvironmentConfig `yaml:"environment"`
	Service     map[string]ServiceConfig     `yaml:"service"`
}

type GlobalConfig struct {
	ChartsDir      string `yaml:"charts_dir"`
	AWSIAMRoleName string `yaml:"aws_iam_role_name"`
	AWSRegion      string `yaml:"aws_region"`
}

type EnvironmentConfig struct {
	AWSAccountID string `yaml:"aws_account_id"`
	Deploy       string `yaml:"deploy"` // "auto" | "pr"
	Tag          string `yaml:"tag"`    // "version" | "sha"
}

type ServiceConfig struct {
	ECRRepository string `yaml:"ecr_repository"`
	Release       string `yaml:"release"`       // "auto" | "gated"
	Deploy        string `yaml:"deploy"`        // overrides environment-level
	Tag           string `yaml:"tag"`           // overrides environment-level
	TagPrefix     string `yaml:"tag_prefix"`
	VersionStrategy string `yaml:"version_strategy"`
}

// Environment is a fully-resolved environment entry for a specific service.
type Environment struct {
	Name         string
	Deploy       string // "auto" | "pr"
	Tag          string // "version" | "sha"
	AWSAccountID string
	AWSRegion    string
}

// Load reads and parses the matrix config file at path.
func Load(path string) (*MatrixConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg MatrixConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}

// Resolve returns the list of environments for the given service,
// applying merge precedence: global < environment < service.
func (c *MatrixConfig) Resolve(service string) ([]Environment, error) {
	svcCfg, ok := c.Service[service]
	if !ok {
		return nil, fmt.Errorf("service %q not found in config", service)
	}

	var envs []Environment
	for envName, envCfg := range c.Environment {
		e := Environment{
			Name:         envName,
			Deploy:       envCfg.Deploy,
			Tag:          envCfg.Tag,
			AWSAccountID: envCfg.AWSAccountID,
			AWSRegion:    c.Global.AWSRegion,
		}
		// Service-level overrides
		if svcCfg.Deploy != "" {
			e.Deploy = svcCfg.Deploy
		}
		if svcCfg.Tag != "" {
			e.Tag = svcCfg.Tag
		}
		envs = append(envs, e)
	}
	return envs, nil
}
