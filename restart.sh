make docker
sudo docker save -o ~/Documents/rimedo_software/rimedo-ts/rimedo-ts.tar onosproject/rimedo-ts:v0.0.5
sudo ctr -n k8s.io i import ~/Documents/rimedo_software/rimedo-ts/rimedo-ts.tar
kubectl scale deployment/rimedo-ts -n riab --replicas=0
kubectl scale deployment/rimedo-ts -n riab --replicas=1
