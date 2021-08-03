#!groovy

milestone 0
timestamps {
    node('docker') {
        checkout scm

        docker.withRegistry('https://harbor.cyverse.org', 'jenkins-harbor-credentials') {
            def dockerImage
            stage('Build') {
                milestone 50
                try {
                    dockerImage = docker.build("harbor.cyverse.org/de/vice-file-transfers:${env.BUILD_TAG}", "--build-arg porklock_tag=${env.BRANCH_NAME} .")
                } catch {
                    dockerImage = docker.build("harbor.cyverse.org/de/vice-file-transfers:${env.BUILD_TAG}")
                }
                milestone 51
                dockerImage.push()
            }
            stage('Test') {
                try {
                    sh "docker create --name ${dockerTestRunner} ${dockerRepo}"
                    sh "docker cp ${dockerTestRunner}:/test-results.xml ."
                    sh "docker rm ${dockerTestRunner}"
                } finally {
                    junit 'test-results.xml'

                    sh "docker run --rm --name ${dockerTestCleanup} -v \$(pwd):/build -w /build alpine rm -r test-results.xml"
                }
            }
            stage('Docker Push') {
                milestone 100
                dockerImage.push("${env.BRANCH_NAME}")
                // Retag to 'qa' if this is master/main (keep both so when it switches this keeps working)
                if ( "${env.BRANCH_NAME}" == "master" || "${env.BRANCH_NAME}" == "main" ) {
                    dockerImage.push("qa")
                }
                milestone 101
            }
        }
    }
}
