# WebsocketReverseProxy
A fault tolerant reverse proxy, used to load balance websockets and http requests.

---
### Features
---
- The load balancer periodically performs health checks to monitor the health status of servers, ensuring that only healthy servers are used to handle incoming traffic.
- Health checks for all servers are performed concurrently, and the list of healthy servers is updated in a thread-safe manner.
- Health check (writer) and user connection requests (readers) are managed using a write-preferred reader-writer mutex, that prevents starvation of health check go routines.