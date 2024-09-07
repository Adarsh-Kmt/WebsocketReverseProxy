# WebsocketReverseProxy
A fault tolerant reverse proxy, used to load balance websockets and http requests.


## Features:

- The load balancer periodically performs health checks to monitor the health status of servers, ensuring that only healthy servers are used to handle incoming traffic.
- Health checks for all servers are performed concurrently, and the list of healthy servers is updated in a thread-safe manner.
- Health check (writer) and user connection requests (readers) are managed using a write-preferred reader-writer mutex, that prevents starvation of health check go routines.


## Load Balancer Setup:

Follow these steps to configure the load balancer for your application using an `.ini` file.


1. **Create an `.ini` file:**

   - Create a new `.ini` file to store your load balancer configuration.

2. **Specify Frontend Settings:**

   - Under the `[frontend]` section of the `.ini` file, specify the `host` and `port` for the load balancer to run.
     
3. **Specify Websocket Server Settings:**
   
   - Under the `[websocket]` section, define your WebSocket servers. Use the format `serverN={host:port}` to list each server.
     
4. **Specify HTTP Server Settings:**
   
   - Under the `[http]` section, define your HTTP servers. Use the format `serverN={host:port}` to list each server.

5. **Docker Run Command:**

   - Execute the following docker command to create and run the reverse proxy container:

     ```powershell
     docker run -v {absolutePathToConfigFile}:/app/reverse-proxy-config.ini reverse_proxy_v5_http

   
Example Configuration:
   ```ini
   [frontend]
   host=rp_v5
   port=8084

   [websocket]
   server1=es1_hc:8080
   server2=es2_hc:8080
   server3=es3_hc:8080

   [http]
   server1=es1_hc:8080
   server2=es2_hc:8080
   server3=es3_hc:8080
