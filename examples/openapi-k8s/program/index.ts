import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/openapi-k8s";

const ns = new k8s.V1Namespaces("ns", {
  metadata: {
    name: "ns",
  },
});
