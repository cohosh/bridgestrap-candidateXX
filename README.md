Bridgestrap
===========

Bridgestrap implements an API (for machines) and a Web interface (for people)
to test Tor bridge lines by making a Tor instance fetch the bridges'
descriptor.

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

Clients send one or more bridge lines to the following API, using an HTTP GET
request, and place the bridge lines in the request body:

      https://HOST/bridge-state

The request body must look as follows:

      {"bridge_lines": ["BRIDGE_LINE_1", ..., "BRIDGE_LINE_N"]}

The "BRIDGE_LINE" strings in the list may contain any bridge line (excluding
the "Bridge" prefix) that tor accepts.  Here are a few examples:

* `1.2.3.4:1234`
* `1.2.3.4:1234 1234567890ABCDEF1234567890ABCDEF12345678`
* `obfs4 1.2.3.4:1234 cert=fJRlJc0T7i2Qkw3SyLQq+M6iTGs9ghLHK65LBy/MQewXJpNOKFq63Om1JHVkLlrmEBbX1w iat-mode=0`

You can test bridgestrap's API over the command line as follows:

      curl -X GET localhost:5000/bridge-state -d '{"bridge_lines": ["BRIDGE_LINE"]}'

You can also use the script test-bridge-lines in the "script" directory to test
a batch of bridge lines.

Output
------

The service responds with the following JSON:

      {
        "bridge_results": {
          "BRIDGE_LINE_1": {
            "functional": BOOL,
            "last_tested": "STRING",
            "error": "STRING", (only present if "functional" is false)
          },
          ...
          "BRIDGE_LINE_N": {
            ...
          }
        },
        "error": "STRING", (only present if the entire test failed)
        "time": FLOAT
      }

In a nutshell, the "bridge_results" dictionary maps bridge lines (as they were
provided in the request) to a dictionary consisting of three keys: "functional"
is set to "true" if tor could fetch the bridge's descriptor.  If tor was unable
to fetch the bridge's descriptor, "functional" is set to "false" and the
"error" key maps to an error string.  The key "last_tested" maps to a string
representation (in ISO 8601 format) of the UTC time and date the bridge was
last tested.

In addition to the "bridge_results" dictionary, the response may contain an
optional "error" key if the entire test failed (e.g. if bridgestrap failed to
communicate with its tor instance).  Finally, "time" is a float that represents
the number of seconds that the test took.

Here are a few examples:

    {
      "bridge_results": {
      },
      "error": "something truly ominous happened",
      "time": 0.32
    }

    {
      "bridge_results": {
        "obfs4 1.2.3.4:1234 cert=fJRlJc0T7i2Qkw3SyLQq+M6iTGs9ghLHK65LBy/MQewXJpNOKFq63Om1JHVkLlrmEBbX1w iat-mode=0": {
          "functional": true,
          "last_tested": "2020-11-12T19:42:16.736853122Z"
        },
        "1.2.3.4:1234": {
          "functional": false,
          "last_tested": "2020-11-10T09:44:45.877531581Z",
          "error": "timed out waiting for bridge descriptor"
        }
      },
      "time": 3.1824
    }

    {
      "bridge_results": {
        "1.2.3.4:1234 1234567890ABCDEF1234567890ABCDEF12345678": {
          "functional": false,
          "last_tested": "2020-11-10T09:44:45.877531581Z",
          "error": "timed out waiting for bridge descriptor"
        }
      },
      "time": 0
    }
