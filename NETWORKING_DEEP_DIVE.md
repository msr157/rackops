# Docker Hub Connectivity: The Problem, The Journey & The Final Fix

## The Big Picture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         YOUR NETWORK SETUP                              │
│                                                                         │
│   ┌─────────────┐     ┌──────────────┐     ┌───────────────────────┐   │
│   │  Mini PC    │────▶│  ISP Router  │────▶│  ISP Network (CGNAT)  │   │
│   │ (Dokploy)   │     │  (Vertex)    │     │  Shared Public IP     │   │
│   └─────────────┘     └──────────────┘     └───────────────────────┘   │
│                                                                         │
│   ┌─────────────────────────────────────────────────────────────────┐   │
│   │   ⚠️  TWO FUNDAMENTAL PROBLEMS DISCOVERED                       │   │
│   │   1. You are behind CGNAT (no real public IP)                   │   │
│   │   2. Your ISP actively blocks ALL connections to AWS servers!   │   │
│   └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Problem 1: CGNAT (Why Inbound Port Forwarding is Useless)

**CGNAT = Carrier Grade NAT**. Your ISP gives you a "fake" private IP address that is shared among dozens of customers. You do NOT own a real public IP.

```
                    THE CGNAT PROBLEM
                    
  ┌──────────────────────────────────────────────────────┐
  │                    ISP NETWORK                       │
  │                                                      │
  │   Customer A ──┐                                     │
  │                │   ┌─────────────────┐               │
  │   Customer B ──┼──▶│  CGNAT Router   │──▶ Internet  │
  │   (YOU) 🏠     │   │  (Shared IP)    │               │
  │                │   └─────────────────┘               │
  │   Customer C ──┘                                     │
  └──────────────────────────────────────────────────────┘

  The Problem: If someone on the internet wants to reach YOU,
  they hit the shared IP. The CGNAT router has no idea which
  customer to forward the traffic to. Your Mini PC is invisible!

  ✅ Solution: We bypassed this using Cloudflare Tunnels! The
  Mini PC reaches OUT to Cloudflare, meaning the ISP's CGNAT
  can't block inbound web traffic.
```

---

## Problem 2: ISP Blocks AWS (The True Villain)

This was the most shocking discovery of the session. The network diagnosis (`curl -v`) revealed that your ISP is performing Deep Packet Inspection (DPI) and actively terminating connections to Amazon Web Services (AWS) IP addresses.

```
  ┌──────────────────────────────────────────────────────────────┐
  │               WHAT HAPPENS WHEN DOCKER PULLS                 │
  │                                                              │
  │  1. Docker asks: "What is registry-1.docker.io?"             │
  │     DNS replies: "It's at 54.89.109.211 (An AWS IP)"         │
  │                                                              │
  │  2. Docker connects to AWS over HTTPS (Port 443)             │
  │                                                              │
  │     Mini PC ──▶ Router ──▶ ISP Network ──▶ ❌ BLOCKED!      │
  │                                                              │
  │  3. Connection HANGS, then gives "TLS handshake timeout"     │
  └──────────────────────────────────────────────────────────────┘
```

---

## The Failed Fixes (And Why They Failed)

We tried several highly advanced networking tricks, and the ISP defeated almost all of them because of how aggressively they block AWS.

### ❌ Attempt 1: MTU Size / MSS Clamping (1280)
```
  Theory:  The ISP is dropping large TLS packets silently.
           Forcing the packet size (MTU) smaller will bypass it.
  Result:  FAILED. The ISP drops the entire TCP connection to
           AWS IPs regardless of how small the packets are.
```

### ❌ Attempt 2: TinyProxy on Azure (via Tailscale)
```
  Theory:  Route Docker pull traffic through an Azure VM proxy 
           over the encrypted Tailscale VPN network.
  Result:  FAILED. Why? Because Tailscale's own authentication
           servers (login.tailscale.com) are hosted on AWS!
           The moment we restarted Tailscale, your ISP blocked
           it from authenticating, taking the VPN offline entirely.
```

### ❌ Attempt 3: SOCKS5 SSH Tunnel
```
  Theory:  Create an encrypted SSH tunnel to Azure and force 
           Tailscale to authenticate through it.
  Result:  FAILED. We did not have the correct SSH Private Key 
           on the Mini PC to authenticate to Azure directly.
```

---

## ✅ The Final Fix: Google Cloud Mirror (Checkmate)

After realizing the ISP was specifically hunting AWS connections (which took down Docker Hub and Tailscale), we realized we didn't need a proxy or a VPN to download Docker images. We just needed to download them from a non-AWS server!

**The Fix:** We configured the Docker Daemon on the Mini PC to use Google Cloud's official Docker registry mirror (`mirror.gcr.io`).

  ┌─────────────────────────────────────────────────────────────────┐
  │                    THE FINAL ARCHITECTURE                       │
  │                                                                 │
  │   ┌──────────────────┐                                          │
  │   │    MINI PC       │                                          │
  │   │                  │             (Blocked by ISP)             │
  │   │  ┌────────────┐  │             ❌ Docker Hub (AWS)         │
  │   │  │  Docker    │──┼── pull ──▶                               │
  │   │  │  Daemon    │  │             ✅ Google Cloud Mirror      │
  │   │  └────────────┘  │                (mirror.gcr.io)           │
  │   │                  │                                          │
  │   │  ┌────────────┐  │             ✅ Your Users               │
  │   │  │ Cloudflare │──┼── serve ──▶                            │
  │   │  │  Tunnel    │  │             (Bypasses CGNAT)             │
  │   │  └────────────┘  │                                          │
  │   └──────────────────┘                                          │
  └─────────────────────────────────────────────────────────────────┘
```

### Why this is the ultimate solution:
1. **Zero Proxies:** No complex proxy servers to maintain on Azure.
2. **Zero VPN Dependency:** Docker pulls work even if Tailscale is offline.
3. **Full Speed:** Google Cloud is one of the fastest networks in the world; images will download instantly.
4. **Bypasses ISP Filter:** The ISP is only blocking AWS, so connecting to Google Cloud completely bypasses their deep packet inspection.

All containers deploy normally from Dokploy now. We outsmarted the ISP block without a single network route change!
