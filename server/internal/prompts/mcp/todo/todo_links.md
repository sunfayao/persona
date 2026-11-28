A to-do's saved repository and issue/PR associations. `get` reads them with their revisions; no Git or network call. `add_links` adds 1–50 exact issue or PR URLs as `links` [{url, title}] (actual titles only), grouped by repository by itself. `set` replaces the repositories at once (`expected_revision`, `expected_catalog_revision`, `repositories`; [] removes them) and `set_links` one repository's links (`repo_id`, `expected_revision`, `links`); reread and reconcile on a conflict. The hub already links the issues and PRs an Agent's replies name, and an association never proves completion.

```json
{
  "type": "object",
  "required": [
    "action",
    "id"
  ],
  "properties": {
    "action": {
      "type": "string",
      "enum": [
        "get",
        "add_links",
        "set",
        "set_links"
      ],
      "description": "get, add issue/PR links, replace the repositories, or replace one repository's links"
    },
    "id": {
      "type": "string",
      "description": "the to-do's id"
    },
    "expected_revision": {
      "type": "integer",
      "minimum": 0
    },
    "expected_catalog_revision": {
      "type": "integer",
      "minimum": 0
    },
    "repositories": {
      "type": "array",
      "maxItems": 20,
      "items": {
        "type": "object",
        "required": [
          "repo_id"
        ],
        "properties": {
          "repo_id": {
            "type": "string"
          },
          "directory_alias": {
            "type": "string"
          },
          "requested_ref": {
            "type": "object",
            "properties": {
              "kind": {
                "type": "string",
                "enum": [
                  "default",
                  "branch",
                  "tag",
                  "commit",
                  "ref"
                ]
              },
              "value": {
                "type": "string",
                "minLength": 1
              }
            },
            "additionalProperties": false,
            "required": [
              "kind"
            ],
            "oneOf": [
              {
                "type": "object",
                "properties": {
                  "kind": {
                    "type": "string",
                    "const": "default"
                  }
                }
              },
              {
                "required": [
                  "value"
                ]
              }
            ]
          },
          "links": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "url": {
                  "type": "string",
                  "minLength": 1
                },
                "kind": {
                  "type": "string",
                  "enum": [
                    "issue",
                    "pull_request"
                  ]
                },
                "number": {
                  "type": "string",
                  "pattern": "^[1-9][0-9]{0,19}$"
                },
                "title": {
                  "type": "string",
                  "description": "Plain source title; trim; ≤1024 UTF-8 bytes.",
                  "maxLength": 1024
                }
              },
              "additionalProperties": false,
              "oneOf": [
                {
                  "required": [
                    "url"
                  ],
                  "not": {
                    "anyOf": [
                      {
                        "required": [
                          "kind"
                        ]
                      },
                      {
                        "required": [
                          "number"
                        ]
                      }
                    ]
                  }
                },
                {
                  "required": [
                    "kind",
                    "number"
                  ],
                  "not": {
                    "required": [
                      "url"
                    ]
                  }
                }
              ]
            },
            "maxItems": 50
          }
        },
        "additionalProperties": false
      }
    },
    "repo_id": {
      "type": "string"
    },
    "links": {
      "type": "array",
      "maxItems": 50,
      "items": {
        "type": "object",
        "properties": {
          "url": {
            "type": "string",
            "minLength": 1
          },
          "kind": {
            "type": "string",
            "enum": [
              "issue",
              "pull_request"
            ]
          },
          "number": {
            "type": "string",
            "pattern": "^[1-9][0-9]{0,19}$"
          },
          "title": {
            "type": "string",
            "description": "Plain source title; trim; ≤1024 UTF-8 bytes.",
            "maxLength": 1024
          },
          "role": {
            "type": "string",
            "description": "delivery (default): this task's own deliverable, verified by the delivery gate; reference: related work, shown but never verified at acceptance.",
            "enum": [
              "delivery",
              "reference"
            ]
          }
        },
        "additionalProperties": false,
        "oneOf": [
          {
            "required": [
              "url"
            ],
            "not": {
              "anyOf": [
                {
                  "required": [
                    "kind"
                  ]
                },
                {
                  "required": [
                    "number"
                  ]
                }
              ]
            }
          },
          {
            "required": [
              "kind",
              "number"
            ],
            "not": {
              "required": [
                "url"
              ]
            }
          }
        ]
      }
    },
    "urls": {
      "type": "array",
      "minItems": 1,
      "maxItems": 50,
      "items": {
        "type": "string"
      }
    }
  },
  "additionalProperties": false
}
```
