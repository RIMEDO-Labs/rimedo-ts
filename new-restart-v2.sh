make docker
sudo docker push localhost:5000/rimedo-ts:v0.0.5
kubectl scale deployment/sd-ran-rimedo-ts -n riab --replicas=0
kubectl scale deployment/sd-ran-rimedo-ts -n riab --replicas=1
