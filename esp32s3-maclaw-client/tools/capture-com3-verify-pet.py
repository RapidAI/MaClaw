import serial
import time
port=serial.Serial("COM3",115200,timeout=.2)
end=time.time()+55
chunks=[]
while time.time()<end:
    data=port.read(4096)
    if data: chunks.append(data)
port.close()
open("serial-com3-pet-retry-verify.log","wb").write(b"".join(chunks))
print(sum(map(len,chunks)))
