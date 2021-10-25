# v1.1.0

## Features

* You can now limit what containers Dotege will monitor by specifying the
  `DOTEGE_PROXYTAG` env var. Only containers with a matching `com.chameth.proxytag`
  label will then be used when generating templates.
* You can now use build tags when compiling Dotege to restrict it to a single
  DNS providers for ACME authentications. For example building with
  `-tags lego_httpreq` only includes HTTPREQ and shaves around 30MB from the
  resulting binary.

## Other changes

* Update to Go 1.17
* Miscellaneous dependency updates
