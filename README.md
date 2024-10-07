# WebsocketReverseProxy
A light weight fault tolerant reverse proxy, used to load balance websockets and http requests.


## Features:

- Only 26.21 MB in size!!
- The load balancer periodically performs health checks to monitor the health status of servers, ensuring that only healthy servers are used to handle incoming traffic.
- Health checks for all servers are performed concurrently, and the list of healthy servers is updated in a thread-safe manner.
- Health check (writer) and user connection requests (readers) are managed using a write-preferred reader-writer mutex, that prevents starvation of health check go routines.
- Worker pool pattern used to maintain persistent TCP connections for each server, with each ”Worker” goroutine handling a connection to the server.
- The number of workers may increase/decrease dynamically based on the load on the load balancer and idle time.
- The number of worker go routines may:
    - **Increase** if the buffered channel becomes full, and the timeout has expired.
    - **Decrease** if a worker remains idle for a configurable amount of time.
- The load balancer also has support for Websocket connections. Two go routines per connection are used, one to read from client and write to server, and the 2nd goroutine does the opposite.

## Load Balancer Setup:

Follow these steps to configure the load balancer for your application using an `.ini` file.


1. **Create an `.ini` file:**

   - Create a new `.ini` file to store your load balancer configuration.

2. **Specify Frontend Settings:**

   - Under the `[frontend]` section of the `.ini` file, specify the `host` and `port` for the load balancer to run.
     
3. **Specify Websocket Server Settings:**
   
   - Under the `[websocket]` section, define your WebSocket servers. 
   - Use `algorithm={round-robin/random}` to specify load balancing algorithm. (random load balancing algorithm used by default)
   - Use the format `serverN={host:port}` to list each server.

4. **Specify HTTP Server Settings:**
   
   - Under the `[http]` section, define your HTTP servers.
   - Use `algorithm={round-robin/random}` to specify load balancing algorithm. (random load balancing algorithm used by default)
   - Use the format `serverN={host:port}` to list the address of each server.
   - Use `serverN_max_workers=X` to specify the maximum number of workers/TCP connections per server.
   - Use `serverN_min_workers=Y` to specify the minimum number of workers/TCP connections to be maintained per server.
   - Use `serverN_worker_timeout=Z` to specify the timeout (in seconds) after which an idle worker/TCP connection will terminate.
   - Use `serverN_buffer_size=W` to specify the maximum number of requests that can be queued in a buffer before being sent to the server.

5. **Create Health Check Endpoint:**

   - Create a `/healthCheck` GET endpoint in your HTTP and Websocket Servers, which responds with the following json as a response:
     
     ```json
     {
     	"status" : "HTTP status code"
     }

5. **Docker Run Command:**

   - Execute the following docker command to create and run the reverse proxy container:

     ```powershell
     docker run -v {absolutePathToConfigFile}:/prod/reverse-proxy-config.ini reverse_proxy_v11_var_buf

## Default Configuration Values

|  Configuration  |  Default Value  |
|:---------------:|:---------------:|
|    algorithm    |     random      |
|   max_workers   |       3         |
|   min_workers   |       1         |
|  worker_timeout |       3         |
|   buffer_size   |      10         |


## Example Configuration:

   ```ini
[frontend]
host=rp_v11
port=8080

[websocket]
algorithm=round-robin
server1=es1_hc:8080
server2=es2_hc:8080
server3=es3_hc:8080

[http]
algorithm=random
#server 1 configuration
server1_addr=es1_hc:8080
server1_max_workers=10
server1_min_workers=3
server1_worker_timeout=3
server1_buffer_size=20
#server 2 configuration
server2_addr=es2_hc:8080
server2_max_workers=10
server2_min_workers=3
server2_worker_timeout=3
server2_buffer_size=20
#server 3 configuration
server3_addr=es3_hc:8080
server3_max_workers=10
server3_min_workers=3
server3_worker_timeout=3
server3_buffer_size=20
   ```


## License:

```
MIT License

Copyright (c) 2024 Adarsh Kamath

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

