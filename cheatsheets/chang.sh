#!/bin/bash
for files in `ls _* -d`
do
    # 截取文件名
    fname=${files:1:20}
    # 更改文件名
    echo "$files --> $fname" 
    mv $files $fname
done
