package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type SystemInfo struct {
	Hostname string  `json:"hostname"`
	OS       string  `json:"os"`
	Platform string  `json:"platform"`
	Uptime   uint64  `json:"uptime_seconds"`
	CPUUsage float64 `json:"cpu_usage_percent"`
	MemTotal uint64  `json:"mem_total"`
	MemUsed  uint64  `json:"mem_used"`
	MemFree  uint64  `json:"mem_free"`
	MemPct   float64 `json:"mem_percent"`
}

type ContainerInfo struct {
	ID     string   `json:"id"`
	Names  []string `json:"names"`
	Image  string   `json:"image"`
	State  string   `json:"state"`
	Status string   `json:"status"`
}

type MetricsResponse struct {
	System     SystemInfo      `json:"system"`
	Containers []ContainerInfo `json:"containers"`
}

func generateCertificates(certPath, keyPath string) error {
	log.Println("Generating self-signed certificate...")
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Homelab Mapper Agent"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour), // 1 Year
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	log.Println("Self-signed certificate generated.")
	return nil
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	expectedToken := os.Getenv("AUTH_TOKEN")
	return func(w http.ResponseWriter, r *http.Request) {
		if expectedToken == "" {
			log.Println("WARNING: AUTH_TOKEN is not set, API is open!")
		} else {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token != expectedToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	}
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	var resp MetricsResponse

	// 1. Get System Info
	hostStat, err := host.Info()
	if err == nil {
		resp.System.Hostname = hostStat.Hostname
		resp.System.OS = hostStat.OS
		resp.System.Platform = hostStat.Platform
		resp.System.Uptime = hostStat.Uptime
	}

	cpuPercents, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercents) > 0 {
		resp.System.CPUUsage = cpuPercents[0]
	}

	memStat, err := mem.VirtualMemory()
	if err == nil {
		resp.System.MemTotal = memStat.Total
		resp.System.MemUsed = memStat.Used
		resp.System.MemFree = memStat.Free
		resp.System.MemPct = memStat.UsedPercent
	}

	// 2. Get Docker Containers
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Error creating docker client: %v", err)
	} else {
		defer cli.Close()
		containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
		if err != nil {
			log.Printf("Error listing containers: %v", err)
		} else {
			for _, c := range containers {
				resp.Containers = append(resp.Containers, ContainerInfo{
					ID:     c.ID[:10],
					Names:  c.Names,
					Image:  c.Image,
					State:  c.State,
					Status: c.Status,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	certPath := "server.crt"
	keyPath := "server.key"

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		err := generateCertificates(certPath, keyPath)
		if err != nil {
			log.Fatalf("Failed to generate certificates: %v", err)
		}
	}

	http.HandleFunc("/metrics", authMiddleware(metricsHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8443"
	}

	log.Printf("Agent starting on port %s...", port)
	
	server := &http.Server{
		Addr:    ":" + port,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	
	err := server.ListenAndServeTLS(certPath, keyPath)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
