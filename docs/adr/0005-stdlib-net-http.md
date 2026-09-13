# 5. Use Go Standard Library net/http for Web Server

We decided to use the Go 1.22+ standard library `net/http` rather than external web frameworks such as Gin or Fiber. Go 1.22's `http.ServeMux` provides native HTTP method routing and URL path parameter extraction without adding external dependencies. The standard library offers superior, predictable control over chunked HTTP streaming, range request processing, and flusher controllers.
