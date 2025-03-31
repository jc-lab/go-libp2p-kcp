// Copyright 2025 JC-Lab.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scop

import "time"

// Config 는 연결에 대한 모든 설정을 포함합니다
type Config struct {
	MaxRetries        int
	InitialRTO        time.Duration
	MaxRTO            time.Duration
	KeepaliveInterval time.Duration // Keepalive 패킷을 보내는 주기
	KeepaliveTimeout  time.Duration // Keepalive 응답을 기다리는 최대 시간
}

// DefaultConfig 는 기본 설정값을 반환합니다
func DefaultConfig() *Config {
	return &Config{
		MaxRetries:        5,
		InitialRTO:        time.Second,
		MaxRTO:            time.Second * 16,
		KeepaliveInterval: time.Second * 30, // 30초마다 keepalive
		KeepaliveTimeout:  time.Second * 90, // 90초 동안 응답 없으면 연결 종료
	}
}

// Option 은 Config를 수정하는 함수 타입입니다
type Option func(*Config)

// WithMaxRetries 는 최대 재시도 횟수를 설정합니다
func WithMaxRetries(retries int) Option {
	return func(c *Config) {
		if retries > 0 {
			c.MaxRetries = retries
		}
	}
}

// WithInitialRTO 는 초기 재전송 타임아웃을 설정합니다
func WithInitialRTO(rto time.Duration) Option {
	return func(c *Config) {
		if rto > 0 {
			c.InitialRTO = rto
		}
	}
}

// WithMaxRTO 는 최대 재전송 타임아웃을 설정합니다
func WithMaxRTO(rto time.Duration) Option {
	return func(c *Config) {
		if rto > 0 {
			c.MaxRTO = rto
		}
	}
}

// WithKeepaliveInterval sets the keepalive interval
func WithKeepaliveInterval(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.KeepaliveInterval = d
		}
	}
}

// WithKeepaliveTimeout sets the keepalive timeout
func WithKeepaliveTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.KeepaliveTimeout = d
		}
	}
}
