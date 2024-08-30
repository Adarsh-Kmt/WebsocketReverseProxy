# WebsocketReverseProxy
A fault tolerant reverse proxy, used to load balance websockets and http requests.

---
### Features
---
- The load balancer periodically performs health checks to monitor the health status of end servers, ensuring that only healthy servers are used to handle incoming traffic.
- Health checks for all end servers are performed concurrently, and the list of healthy end servers is updated in a thread-safe manner.
- Health checks (writer) and user connection requests (readers) are managed using reader-writer mutexes.