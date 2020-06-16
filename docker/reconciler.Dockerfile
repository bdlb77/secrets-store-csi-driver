FROM debian:9

RUN apt-get update \
  && apt-get install -y ca-certificates cifs-utils \
  && apt-get install -y curl

LABEL description="Secrets Store Reconciler"

COPY ./_output/rotation-reconciler /rotation-reconciler
