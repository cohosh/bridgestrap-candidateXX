Bridgestrap
===========

Bridgestrap implements a API (for machines) and a Web interface (for people) to
test a given bridge line by spawning a tor instance and having it bootstrap
over the bridge line.

Installation
------------

First, install the Golang binary:

      go install

Then, run the binary:

      bridgestrap

By default, bridgestrap will listen on port 5000.  To use its Web interface
(don't forget to turn it on by using the `-web` switch), point your browser to
the address and port that bridgestrap is listening on.  Use the argument
`-addr` to listen to a custom address and port.

Input
-----

Clients send bridge lines to the following API, using an HTTP GET request, and
place the bridge line in the request body:

      https://HOST/bridge-state

The request body must look as follows:

      {"bridge_line": "BRIDGE_LINE"}

The value of "bridge_line" can be any bridge line (excluding the "Bridge"
prefix) that tor accepts.  Here are a few examples:

* `1.2.3.4:1234`
* `1.2.3.4:1234 1234567890ABCDEF1234567890ABCDEF12345678`
* `obfs4 1.2.3.4:1234 cert=fJRlJc0T7i2Qkw3SyLQq+M6iTGs9ghLHK65LBy/MQewXJpNOKFq63Om1JHVkLlrmEBbX1w iat-mode=0`

You can test bridgestrap's API over the command line as follows:

      curl -X GET localhost:5000/bridge-state -d '{"bridge_line": "BRIDGE_LINE"}'

Output
------

The service responds with the following JSON:

      {
        "functional": BOOL,
        "error": "STRING", (only present if "functional" is false.)
        "time": FLOAT
      }

If tor could bootstrap over the given bridge line, "functional" is "true" and
"false" otherwise.  If "functional" is "false", "error" will contain an error
string.  "time" is a float that represents the number of seconds that
bridgestrap's test took.

Here are a few examples:

    {
      "functional":false,
      "error":"Invalid JSON request.",
      "time":0
    }

    {
      "functional":false,
      "error":"Oct 23 17:36:57.000 [warn] Problem bootstrapping. Stuck at 10%: Finishing handshake with directory server. (DONE; DONE; count 1; recommendation warn; host [REDACTED])",
      "time":32.31
    }

    {
      "functional":false,
      "error":"Oct 23 17:34:57.680 [warn] Too few items to Bridge line.",
      "time":0.013
    }

    {
      "functional":true,
      "time":13.161
    }
