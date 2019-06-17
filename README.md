# Thanos Receive Controller

This controller configures multiple Thanos receiver hashrings running as StatefulSets on Kubernetes.  

The entire lifecycle starts with a given and manually configured hashring configuration. 
That configuration is read by this controller and updated in-memory once it changes.
Concurrently this controller starts listing/watching all StatefulSets with certain labels. 

Each StatefulSet has a number of replicas configured, 
which we can use to compute the full URIs for its Pods inside the Kubernetes cluster.
Once we know this list of endpoints/targets for a hashring,
we can start populating the manual configuration with these endpoints and write a updated second ConfigMap,
which is read by the receivers themselves.
