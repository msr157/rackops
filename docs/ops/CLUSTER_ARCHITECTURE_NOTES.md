# Dokploy & Docker Swarm Cluster Architecture Notes

This document serves as a reference for architectural decisions, limitations, and requirements when building a multi-node cluster using Dokploy (which is powered by Docker Swarm).

## 1. The "Quorum" Rule (Why we need 3 servers for High Availability)

In a Docker Swarm cluster, the "Manager" nodes act as the brain. They vote on every decision (deploying apps, restarting containers, etc.). To make a decision, a **strict majority** (a Quorum) of the managers must agree.

- **1 Server:** You have 1 vote. If it goes down, everything goes down.
- **2 Servers:** A majority of 2 is still 2 (you need >50%). If one server goes offline, you only have 1 vote left, which is *not* a majority. The cluster instantly locks itself into "Read-Only" mode to prevent data corruption (split-brain). Your apps keep running, but the Dokploy Dashboard freezes and you cannot deploy updates.
- **3 Servers:** A majority of 3 is 2. If one server dies, you still have 2 managers online. They hold the majority, and the Dokploy Dashboard stays perfectly online.

**Takeaway:** For true High Availability of the control panel/dashboard, you *must* have an odd number of Manager nodes (3, 5, 7). 

## 2. The Registry Requirement

When deploying applications in a Dokploy Cluster, you **must** configure a Docker Registry (like Docker Hub, GitHub Container Registry, etc.).

**Why?**
When you deploy an application in a cluster, the servers do *not* act independently:
1. Only the **Primary Manager Node** pulls the code from GitHub and builds the Docker Image.
2. The other Worker/Manager nodes **do not build the code**. They simply wait for the finished image.
3. Because the other nodes don't build the code themselves, the Primary node must upload the finished image to a central "cloud drive" (The Docker Registry).
4. The other nodes then download the finished image from the Registry and run it.

If you do not provide a Registry, the other nodes will look on their local hard drives for the image, fail to find it, and crash with an "Image Not Found" error.

## 3. Alternative: Independent Cloned Servers

If you absolutely do not want to use a Docker Registry, you cannot use the Docker Swarm "Cluster" feature.

Instead, you must use **Independent Cloned Servers**:
- You install a completely separate, standalone Dokploy instance on every server.
- You connect GitHub to every single server.
- Every server independently pulls the code, builds its own image, and runs it.
- You put a Load Balancer (like Cloudflare) in front of them to route traffic.

**Trade-offs:** 
- *Pro:* No Docker Registry required. No complex VPN/Tailscale required for Swarm communication.
- *Con:* You have to manually log into 3 different Dokploy dashboards and click "Deploy" 3 separate times every time you push a code update. A true Swarm Cluster only requires you to click deploy *once*.
