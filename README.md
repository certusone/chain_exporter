# chain_exporter
Metrics exporter and alerter tools for cosmos-sdk.

[Subscribe to our newsletter](https://mailchi.mp/38ac109a9ab2/certusone) for updates on this project,
insights on the upcoming Game of Stakes and more.

## chain_exporter

Chain_exporter exports blockchain metadata, information about missed blocks and governance proposals from the lcd to Postgres.

Environment:
```
"GAIA_URL" = "http://gaia-node1:26657" # gaia URL
"DB_HOST" = "postgres-chain:5432" # Postgres host:port
"DB_NAME" = "postgres" # DB name
"DB_USER" = "postgres" # DB username
"DB_PW"= "mypwd" # DB password
"LCD_URL" = "https://gaia-lcd:1317" # gaia lcd URL
```

## net_exporter

Net_exporter periodically exports net_info from gaia to Postgres.
This allows to get an extensive overview of the current network and connectivity status of the cosmos-sdk.

Environment:
```
"GAIA_URLs" = "http://gaia-node0:26657,http://gaia-node1:26657" # gaia URLs (comma-separated)
"DB_HOST" = "postgres-chain:5432" # Postgres host:port
"DB_NAME" = "postgres" # DB name
"DB_USER" = "postgres" # DB username
"DB_PW"= "mypwd" # DB password
"PERIOD" = "10" # Period to save data in seconds
```

## alerter

Alerter forwards missed block and governance alerts stored in postgres to Sentry.

Environment:
```
"DB_HOST" = "postgres-chain:5432" # Postgres host:port
"DB_USER" = "postgres" # DB username
"DB_NAME" = "postgres" # DB name
"DB_PW"= "mypwd" # DB password
"RAVEN_DSN" = "http://xxxxxxx" # DSN_URL from Sentry (hosted or self-hosted)
"ADDRESS" = "ABCDDED" # Address of the validator to alert on
```
