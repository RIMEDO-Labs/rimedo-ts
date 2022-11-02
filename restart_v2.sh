make docker
sudo docker save -o ~/Documents/rimedo_labs/rimedo-ts/rimedo-ts.tar onosproject/rimedo-ts:v0.0.5
sudo ctr -n k8s.io i import ~/Documents/rimedo_labs/rimedo-ts/rimedo-ts.tar
kubectl scale deployment/sd-ran-rimedo-ts -n riab --replicas=0
kubectl scale deployment/sd-ran-rimedo-ts -n riab --replicas=1
