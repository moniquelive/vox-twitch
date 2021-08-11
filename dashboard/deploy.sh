docker build -t 867132728191.dkr.ecr.us-east-2.amazonaws.com/vox-twitch:latest .

aws ecr get-login-password --region us-east-2 | \
  docker login \
  --username AWS \
  --password-stdin \
  867132728191.dkr.ecr.us-east-2.amazonaws.com

docker push 867132728191.dkr.ecr.us-east-2.amazonaws.com/vox-twitch:latest
